package main

import (
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/dyatlov/go-htmlinfo/htmlinfo"
	"github.com/gofrs/uuid"
	"github.com/gosimple/slug"
)

type Identifier string

type PostType int

const (
	TypeDefault PostType = 0
	TypeRepost  PostType = 1
	TypeHeart   PostType = 2
	TypeAll     PostType = 3
)

var (
	ErrURIUsed         = errors.New("URI in use")
	ErrContentNotFound = errors.New("content not found")
	ErrInvalidID       = errors.New("invalid id")
)

type ContentPiece struct {
	Body                 string
	Snippet              string
	DateCreated          time.Time
	Date                 time.Time
	ID                   Identifier
	ResponseToURL        string
	Title                string
	Type                 PostType
	URI                  string
	ResponseToURLPreview *URLPreview
	Tags                 []string
}

func (c *ContentPiece) HTML() template.HTML {
	return template.HTML(c.Body)
}

func (c *ContentPiece) DateString() string {
	return c.Date.Format("January 2006 2 at 03:04AM")
}

func (c *ContentPiece) DateInputString() string {
	return c.Date.Format("2006-01-02")
}

func (c *ContentPiece) TagString() string {
	return strings.Join(c.Tags, ", ")
}

func (c *ContentPiece) TimeInputString() string {
	return c.Date.Format("15:04")
}

type URLPreview struct {
	OembedHTML   template.HTML
	Snippet      string
	ThumbnailURL string
	Title        string
	URL          string
	DateCrawled  time.Time
}

func (p *URLPreview) IsFulfilled() bool {
	if p.URL == "" {
		return false
	}
	if p.Title != "" && p.Snippet != "" {
		return true
	}
	if p.OembedHTML != "" {
		return true
	}
	return false
}

type PageInfo struct {
	Current   int
	Previous  int
	Next      int
	Total     int
	ItemTotal int
	ItemCount int
	PostType  PostType
	Tag       string
}

func (p *PageInfo) HasPrevious() bool {
	return p.Current > 1
}

func (p *PageInfo) HasNext() bool {
	return p.Current < p.Total
}

func (p *PageInfo) CalculateTotal() {
	p.Total = int(math.Ceil(float64(p.ItemCount) / float64(p.ItemTotal)))
	p.Previous = p.Current - 1
	p.Next = p.Current + 1
}

func CreateSample(tx *sql.Tx) error {
	err := CreateContent(tx, &ContentPiece{
		Title:   "Sample Post",
		Body:    `<p>I am sample</p>`,
		Snippet: "I am sample.",
		Date:    time.Now(),
		URI:     "sample",
		Tags:    []string{"sample"},
	})
	if err == ErrURIUsed {
		return nil
	}
	return err
}

func GetContents(db *sql.DB, page *PageInfo) ([]*ContentPiece, error) {
	args := []interface{}{time.Now()}
	sql := `SELECT COUNT(id) AS count FROM content WHERE date <= ?`
	if page.PostType != TypeAll {
		sql += ` AND type = ?`
		args = append(args, page.PostType)
	}
	stmt, err := db.Prepare(sql)
	if err != nil {
		return nil, err
	}
	var count int
	if err := stmt.QueryRow(args...).Scan(&count); err != nil {
		return nil, err
	}
	if count >= 50 {
		page.ItemCount = 50
	} else {
		page.ItemCount = count
	}
	page.ItemTotal = count
	page.CalculateTotal()

	sql = `
SELECT
	t1.id,
	t1.title,
	t1.body,
	t1.snippet,
	t1.uri,
	t1.date,
	t1.date_created,
	t1.type,
	t1.response_to,
	IFNULL(t2.url, ""),
	IFNULL(t2.title, ""),
	IFNULL(t2.snippet, ""),
	IFNULL(t2.thumbnail_url, ""),
	IFNULL(t2.oembed_html, ""),
	(SELECT IFNULL(GROUP_CONCAT(value, ","), "") FROM tag WHERE id = t1.id) AS tags
FROM
	content AS t1
	LEFT JOIN url_preview AS t2 ON (t1.response_to = t2.url)`

	if page.Tag != "" {
		sql += `INNER JOIN tag AS t3 ON (t1.id = t3.id) `
	}
	sql += `
WHERE
	date <= ?`

	args = []interface{}{time.Now()}
	if page.PostType != TypeAll {
		sql += ` AND t1.type = ?`
		args = append(args, page.PostType)
	}
	if page.Tag != "" {
		sql += ` AND t3.value = ?`
		args = append(args, page.Tag)
	}
	sql += `
ORDER BY
	date DESC
LIMIT ?
OFFSET ?`
	args = append(args, page.ItemCount, (page.Current-1)*page.ItemCount)

	stmt, err = db.Prepare(sql)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	var xs []*ContentPiece
	rows, err := stmt.Query(args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a ContentPiece
		var b URLPreview
		var tags string
		if err := rows.Scan(&a.ID,
			&a.Title,
			&a.Body,
			&a.Snippet,
			&a.URI,
			&a.Date,
			&a.DateCreated,
			&a.Type,
			&a.ResponseToURL,
			&b.URL,
			&b.Title,
			&b.Snippet,
			//&b.DateCrawled,
			&b.ThumbnailURL,
			&b.OembedHTML,
			&tags); err != nil {
			return nil, err
		}
		if tags != "" {
			a.Tags = strings.Split(tags, ",")
		}
		if a.ResponseToURL != "" {
			a.ResponseToURLPreview = &b
		}
		fmt.Println(a.Title)
		xs = append(xs, &a)
	}
	return xs, nil
}

func GetContent(tx *sql.Tx, uri string) (*ContentPiece, error) {
	stmt, err := tx.Prepare(`
SELECT
	t1.id,
	t1.title,
	t1.body,
	t1.snippet,
	t1.uri,
	t1.date,
	t1.date_created,
	t1.type,
	t1.response_to,
	IFNULL(t2.url, ""),
	IFNULL(t2.title, ""),
	IFNULL(t2.snippet, ""),
	IFNULL(t2.thumbnail_url, ""),
	IFNULL(t2.oembed_html, ""),
	(SELECT IFNULL(GROUP_CONCAT(value, ","), "") FROM tag WHERE id = t1.id) AS tags
FROM
	content AS t1
	LEFT JOIN url_preview AS t2 ON (t1.response_to = t2.url)
WHERE
	uri = ?`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	var a ContentPiece
	var b URLPreview
	var tags string
	err = stmt.QueryRow(uri).Scan(&a.ID,
		&a.Title,
		&a.Body,
		&a.Snippet,
		&a.URI,
		&a.Date,
		&a.DateCreated,
		&a.Type,
		&a.ResponseToURL,
		&b.URL,
		&b.Title,
		&b.Snippet,
		//&b.DateCrawled,
		&b.ThumbnailURL,
		&b.OembedHTML,
		&tags)
	if err == sql.ErrNoRows {
		return nil, ErrContentNotFound
	} else if err != nil {
		return nil, err
	}
	if tags != "" {
		a.Tags = strings.Split(tags, ",")
	}
	if a.ResponseToURL != "" {
		a.ResponseToURLPreview = &b
	}
	return &a, nil
}

func CreateContent(tx *sql.Tx, c *ContentPiece) error {
	ok, err := IsAvailableURI(tx, c.URI)
	if err != nil {
		return err
	}
	if !ok {
		return ErrURIUsed
	}

	id, err := uuid.NewV4()
	if err != nil {
		return err
	}
	c.ID = Identifier(id.String())

	// Fetch url info if it exists
	if c.ResponseToURL != "" {
		if _, err := GetURLPreview(tx, c.ResponseToURL); err == sql.ErrNoRows {
			p, err := ScrapURLPreview(c.ResponseToURL)
			if err != nil {
				return err
			}
			if err := PutURLPreview(tx, *p); err != nil {
				return err
			}
		}
	}

	stmt, err := tx.Prepare(`
INSERT INTO content (
	title,
	body,
	snippet,
	date,
	date_created,
	id,
	response_to,
	type,
	uri
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	if _, err := stmt.Exec(c.Title, c.Body, c.Snippet, c.Date, time.Now(), c.ID, c.ResponseToURL, c.Type, c.URI); err != nil {
		return err
	}

	for _, tag := range c.Tags {
		if err := InsertTag(tx, c.ID, tag); err != nil {
			return err
		}
	}
	return nil
}

func InsertTag(tx *sql.Tx, id Identifier, tag string) error {
	stmt, err := tx.Prepare(`INSERT INTO tag (id, value) VALUES (?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	if _, err := stmt.Exec(id, strings.TrimSpace(tag)); err != nil {
		return err
	}
	return nil
}

func DeleteTags(tx *sql.Tx, id Identifier) error {
	stmt, err := tx.Prepare(`DELETE FROM tag WHERE id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(id)
	return err
}

func UpdateContent(tx *sql.Tx, c *ContentPiece, rescrape bool) error {
	if c.ID == "" {
		return ErrInvalidID
	}
	x, err := GetContent(tx, c.URI)
	if err != nil && err != ErrContentNotFound {
		return err
	}
	if x != nil && x.ID != c.ID {
		return ErrURIUsed
	}

	// It changed!
	if c.ResponseToURL != "" && ((x != nil && c.ResponseToURL != x.ResponseToURL) || rescrape) {
		if p, err := GetURLPreview(tx, c.ResponseToURL); err == sql.ErrNoRows || rescrape {
			fmt.Println("Scrapping")
			p, err := ScrapURLPreview(c.ResponseToURL)
			if err != nil {
				return err
			}
			if err := PutURLPreview(tx, *p); err != nil {
				return err
			}
			fmt.Println("Scraped it")
			fmt.Println(p)
		} else if err != nil {
			fmt.Println(2)
			return err
		} else {
			fmt.Println("already has preview")
			fmt.Println(p)
		}
	}

	stmt, err := tx.Prepare(`UPDATE content SET
	title = ?,
	body = ?,
	snippet = ?,
	date = ?,
	response_to = ?,
	uri = ?,
	type = ?
	WHERE id = ?;`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	res, err := stmt.Exec(c.Title, c.Body, c.Snippet, c.Date, c.ResponseToURL, c.URI, c.Type, c.ID)
	if err != nil {
		return err
	}
	if count, err := res.RowsAffected(); err != nil {
		return err
	} else if count != 1 {
		return ErrContentNotFound
	}
	if err := DeleteTags(tx, c.ID); err != nil {
		return err
	}
	for _, tag := range c.Tags {
		if err := InsertTag(tx, c.ID, tag); err != nil {
			return err
		}
	}
	return nil
}

func DeleteContent(tx *sql.Tx, c *ContentPiece) error {
	stmt, err := tx.Prepare(`DELETE FROM content WHERE id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	res, err := stmt.Exec(c.ID)
	if err != nil {
		return err
	}
	if count, err := res.RowsAffected(); err != nil {
		return err
	} else if count == 0 {
		return ErrContentNotFound
	}
	return DeleteTags(tx, c.ID)
}

func TitleToURI(s string) string {
	return slug.Make(s)
}

func IsAvailableURI(tx *sql.Tx, uri string) (bool, error) {
	stmt, err := tx.Prepare(`SELECT COUNT(uri) FROM content WHERE uri = ?`)
	if err != nil {
		return false, err
	}
	defer stmt.Close()
	var count int
	if err := stmt.QueryRow(uri).Scan(&count); err != nil {
		return false, err
	}
	if count > 0 {
		return false, nil
	}
	return true, nil
}

func GetURLPreview(tx *sql.Tx, s string) (*URLPreview, error) {
	var p URLPreview
	stmt, err := tx.Prepare(`SELECT
		url,
		title,
		snippet,
		date_crawled,
		oembed_html,
		thumbnail_url
	FROM url_preview WHERE url = ? LIMIT 1`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	err = stmt.QueryRow(s).Scan(&p.URL, &p.Title, &p.Snippet, &p.DateCrawled, &p.OembedHTML, &p.ThumbnailURL)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func PutURLPreview(tx *sql.Tx, p URLPreview) error {
	stmt, err := tx.Prepare(`DELETE FROM url_preview WHERE url = ?`)
	if err != nil {
		return err
	}
	if _, err := stmt.Exec(p.URL); err != nil {
		return err
	}
	stmt.Close()
	stmt, err = tx.Prepare(`INSERT INTO url_preview (
		url,
		title,
		snippet,
		date_crawled,
		oembed_html,
		thumbnail_url
	) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	if _, err := stmt.Exec(p.URL, p.Title, p.Snippet, p.DateCrawled, string(p.OembedHTML), p.ThumbnailURL); err != nil {
		return err
	}
	stmt.Close()
	return nil
}

func ScrapURLPreview(s string) (*URLPreview, error) {
	resp, err := http.Get(s)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	info := htmlinfo.NewHTMLInfo()
	if err := info.Parse(resp.Body, &s, nil); err != nil {
		return nil, err
	}

	og := info.OGInfo
	p := URLPreview{
		URL:         s,
		Title:       info.Title,
		Snippet:     info.Description,
		DateCrawled: time.Now(),
	}
	if info.OembedInfo != nil {
		if info.OembedInfo.HTML != "" {
			p.OembedHTML = template.HTML(info.OembedInfo.HTML)
		}
		if info.OembedInfo.ThumbnailURL != "" {
			p.ThumbnailURL = info.OembedInfo.ThumbnailURL
		}
	} else if og != nil && len(og.Images) > 0 {
		p.ThumbnailURL = og.Images[0].URL
	}
	if og != nil {
		p.URL = og.URL
		p.Title = og.Title
		p.Snippet = og.Description
	}
	return &p, nil
}

func PrepareDb(db *sql.DB) error {
	_, err := db.Exec(`
	PRAGMA foreign_keys= ON;
	CREATE TABLE IF NOT EXISTS content (
		title STRING,
		body  STRING,
		snippet STRING,
		date DATETIME,
		date_created DATETIME,
		id STRING PRIMARY KEY,
		response_to STRING,
		type STRING,
		uri STRING
	);
	CREATE TABLE IF NOT EXISTS tag (
		id STRING,
		value STRING
	);
	CREATE TABLE IF NOT EXISTS url_preview (
		url STRING PRIMARY KEY,
		title STRING,
		snippet STRING,
		date_crawled DATETIME,
		oembed_html STRING,
		thumbnail_url STRING
	);`)
	return err
}
