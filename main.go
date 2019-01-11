package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"github.com/dyatlov/go-htmlinfo/htmlinfo"
	"github.com/gin-gonic/gin"
	"github.com/gofrs/uuid"
	"github.com/gosimple/slug"
	_ "github.com/mattn/go-sqlite3"
)

type Identifier string

type PostType int

const (
	TypeDefault PostType = 0
	TypeRepost  PostType = 1
	TypeHeart   PostType = 2
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

func (c *ContentPiece) TimeInputString() string {
	return c.Date.Format("15:04")
}

type ContentPayload struct {
	ContentPiece
	DateString      string
	TimeString      string
	TransactionType string
	OldURI          string
	Password        string
	Rescrape        string
}

type URLPreview struct {
	OembedHTML   template.HTML
	Snippet      string
	ThumbnailURL string
	Title        string
	URL          string
	DateCrawled  time.Time
}

func main() {
	var password string
	var dbfile string
	var sampleme bool
	var templateGlob string

	flag.StringVar(&password, "password", "password", "The password to validate editing.")
	flag.StringVar(&dbfile, "dbfile", "./a.db", "The database file to use for SQLite3.")
	flag.StringVar(&templateGlob, "templates", "./templates/*.html", "The template glob to use.")
	flag.BoolVar(&sampleme, "sample", false, "Create the sample post on start up?")
	flag.Parse()

	db, err := sql.Open("sqlite3", dbfile)
	if err != nil {
		panic(err)
	}
	db.SetMaxOpenConns(1)

	if err := PrepareDb(db); err != nil {
		panic(err)
	}

	// Preparation
	tx, err := db.Begin()
	if err != nil {
		panic(err)
	}
	if c, err := GetContent(tx, ""); err == nil {
		DeleteContent(tx, c)
	}
	if sampleme {
		if err := CreateSample(tx); err != nil {
			panic(err)
		}
	}
	if err := tx.Commit(); err != nil {
		panic(err)
	}

	r := gin.Default()

	r.LoadHTMLGlob(templateGlob)

	r.GET("/all", func(c *gin.Context) {
		var page int
		pageStr := c.Query("page")
		if pageInt, err := strconv.Atoi(pageStr); err == nil {
			page = pageInt
			if page <= 0 {
				page = 1
			}
		}
		xs, err := GetContents(db, page)
		if err != nil {
			HandleError(c, err)
			return
		}
		HandleSend(c, "all.html", map[string]interface{}{
			"Items": xs,
		})
	})

	r.GET("/new", func(c *gin.Context) {
		sample := ContentPiece{
			Date: time.Now(),
		}
		HandleSend(c, "editor.html", &sample)
	})

	r.GET("/content/:contentUri", func(c *gin.Context) {
		tx, err := db.Begin()
		if err != nil {
			HandleError(c, err)
			return
		}
		content, err := GetContent(tx, c.Params.ByName("contentUri"))
		if err != nil {
			HandleError(c, err)
			return
		}
		tx.Commit()
		if _, ok := c.GetQuery("edit"); ok {
			HandleSend(c, "editor.html", content)
			return
		}
		HandleSend(c, "content.html", content)
	})

	// Create, update, or delete an author's content
	r.POST("/content", func(c *gin.Context) {
		var res ContentPayload
		if err := c.ShouldBind(&res); err != nil {
			HandleError(c, err)
			return
		}

		fmt.Printf("before uri: '%s' title: '%s'\n", res.URI, res.Title)
		fmt.Println(res)

		if res.Password != password {
			HandleError(c, errors.New("not authorized"))
			return
		}

		if d, err := time.Parse("2006-01-02 15:04", res.DateString+" "+res.TimeString); err != nil {
			HandleError(c, err)
		} else {
			res.Date = d
		}

		if res.ResponseToURL == "" && (res.Type == TypeHeart || res.Type == TypeRepost) {
			HandleError(c, errors.New("missing response url"))
			return
		}

		if res.URI == "" {
			if res.Title != "" {
				res.URI = TitleToURI(res.Title)
			} else {
				res.URI = strconv.FormatInt((time.Now().Unix()), 10)
			}
		}

		fmt.Printf("after uri: '%s' title: '%s'\n", res.URI, res.Title)

		tx, err := db.Begin()
		if err != nil {
			HandleError(c, err)
			return
		}
		switch res.TransactionType {
		case "DELETE":
			err = DeleteContent(tx, &res.ContentPiece)
			break
		case "UPDATE":
			err = UpdateContent(tx, &res.ContentPiece, res.Rescrape == "on")
			break
		default:
			err = CreateContent(tx, &res.ContentPiece)
			break
		}
		if err != nil {
			tx.Rollback() // Log it?
			HandleError(c, err)
			return
		}
		if err := tx.Commit(); err != nil {
			HandleError(c, err)
			return
		}
		if _, ok := c.GetQuery("json"); ok {
			c.JSON(200, res.ContentPiece)
			return
		}
		c.Redirect(301, "/content/"+res.URI)
	})

	r.Run(":8080")
}

func HandleError(c *gin.Context, err error) {
	w := map[string]string{"Error": err.Error()}
	if _, ok := c.GetQuery("json"); ok {
		c.JSON(500, w)
		return
	}
	c.HTML(500, "error.html", w)
}

func HandleSend(c *gin.Context, template string, scope interface{}) {
	if _, ok := c.GetQuery("json"); ok {
		c.JSON(200, scope)
		return
	}
	c.HTML(200, template, scope)
}

func CreateSample(tx *sql.Tx) error {
	err := CreateContent(tx, &ContentPiece{
		Title:   "Sample Post",
		Body:    `<p>I am sample</p>`,
		Snippet: "I am sample.",
		Date:    time.Now(),
		URI:     "sample",
	})
	if err == ErrURIUsed {
		return nil
	}
	return err
}

func GetContents(db *sql.DB, page int) ([]*ContentPiece, error) {
	stmt, err := db.Prepare(`
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
	IFNULL(t2.oembed_html, "")
FROM
	content AS t1
	LEFT JOIN url_preview AS t2 ON (t1.response_to = t2.url)
WHERE
	date <= DATE('now')
ORDER BY
	date DESC
LIMIT 50
OFFSET ?`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	var xs []*ContentPiece
	rows, err := stmt.Query((page - 1) * 50)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var a ContentPiece
		var b URLPreview
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
			&b.OembedHTML); err != nil {
			return nil, err
		}
		if a.ResponseToURL != "" {
			a.ResponseToURLPreview = &b
		}
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
	IFNULL(t2.oembed_html, "")
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
		&b.OembedHTML)
	if err == sql.ErrNoRows {
		return nil, ErrContentNotFound
	} else if err != nil {
		return nil, err
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
	return nil
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
	fmt.Println(1)

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
	return nil
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
