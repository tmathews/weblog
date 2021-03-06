package main

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/disintegration/imaging"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

type M map[string]interface{}

type ContentPayload struct {
	ContentPiece
	DateString      string
	TimeString      string
	TransactionType string
	OldURI          string
	Password        string
	Rescrape        string
	TagString       string
}

var (
	ErrNoAuth = errors.New("not authorized")
)

func parseQueryInt(v string, d, min int) int {
	if i, err := strconv.Atoi(v); err == nil {
		if i < min {
			return d
		} else {
			return i
		}
	}
	return d
}

func GetPage(c *gin.Context) PageInfo {
	var page PageInfo

	page.Current = parseQueryInt(c.Query("page"), 1, 1)
	page.ItemLimit = parseQueryInt(c.Query("limit"), 10, 1)
	if page.ItemLimit > 50 {
		page.ItemLimit = 50
	}

	page.PostType = TypeAll
	if v, ok := c.GetQuery("type"); ok {
		switch v {
		case "post":
			page.PostType = TypeDefault
		case "repost":
			page.PostType = TypeRepost
		case "heart":
			page.PostType = TypeHeart
		case "status":
			page.PostType = TypeStatus
		}
	}

	page.Tag = c.Query("tag")
	page.DateFilter = time.Now()
	if IsAuthorized(c) {
		page.DateFilter = time.Now().AddDate(999, 1, 1)
	}

	return page
}

func IsAuthorized(c *gin.Context) bool {
	s := sessions.Default(c)
	val := s.Get("authed")
	authed, ok := val.(bool)
	return authed && ok
}

func StartServer(db *sql.DB, port int, templateGlob, assetsDir, password, key, cert string) {

	//gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.MaxMultipartMemory = 128 << 20

	r.Use(gin.Logger())
	r.Use(gin.Recovery())
	r.LoadHTMLGlob(templateGlob)

	store := cookie.NewStore([]byte(password))
	r.Use(sessions.Sessions("weblog", store))

	r.NoRoute(func(c *gin.Context) {
		c.HTML(404, "error.html", M{
			"Error": "Page not found.",
		})
	})

	r.GET("/login", func(c *gin.Context) {
		if IsAuthorized(c) {
			c.Redirect(302, "./")
			return
		}
		c.HTML(200, "login.html", nil)
	})

	r.POST("/login", func(c *gin.Context) {
		var payload struct {
			Password string
		}
		if err := c.Bind(&payload); err != nil {
			c.HTML(500, "login.html", M{
				"Error": "issue reading payload",
			})
			return
		}
		if payload.Password != password {
			c.HTML(500, "login.html", M{
				"Error": "invalid password",
			})
			return
		}
		s := sessions.Default(c)
		s.Set("authed", true)
		s.Save()
		c.Redirect(302, "./")
	})

	r.GET("/logout", func(c *gin.Context) {
		s := sessions.Default(c)
		s.Clear()
		s.Save()
		c.Redirect(302, "./")
	})

	r.GET("/", func(c *gin.Context) {
		page := GetPage(c)
		xs, err := GetContents(db, &page)
		if err != nil {
			HandleError(c, err)
			return
		}
		scope := M{
			"Items": xs,
			"Page":  &page,
			"NextQuery": page.QueryString(1),
			"PreviousQuery": page.QueryString(-1),
		}
		if IsReqJSON(c) {
			c.JSON(200, scope)
			return
		}
		scope["Authorized"] = IsAuthorized(c)
		c.HTML(200, "all.html", scope)
	})

	r.GET("/new", func(c *gin.Context) {
		if !IsAuthorized(c) {
			HandleError(c, ErrNoAuth)
			return
		}
		sample := ContentPiece{
			Date: time.Now(),
		}
		var editor string
		switch c.Query("type") {
		case "repost":
			editor = "repost"
		case "heart":
			editor = "heart"
		case "status":
			editor = "status"
		default:
			editor = "editor"
		}
		c.HTML(500, editor+".html", &sample)
	})

	r.GET("/post/:contentUri", func(c *gin.Context) {
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
		if _, ok := c.GetQuery("edit"); ok && IsAuthorized(c) {
			c.HTML(200, "editor.html", content)
			return
		}
		if !IsAuthorized(c) && time.Now().Before(content.Date) {
			HandleError(c, ErrContentNotFound)
			return
		}
		if IsReqJSON(c) {
			c.JSON(200, content)
			return
		}
		c.HTML(200, "post.html", M{
			"Authorized": IsAuthorized(c),
			"Post":       content,
		})
	})

	// Create, update, or delete an author's content
	r.POST("/post", func(c *gin.Context) {
		if !IsAuthorized(c) {
			HandleError(c, ErrNoAuth)
			return
		}

		var res ContentPayload
		if err := c.ShouldBind(&res); err != nil {
			HandleError(c, err)
			return
		}

		if res.DateString == "" || res.TimeString == "" {
			res.Date = time.Now()
		} else if d, err := time.Parse("2006-01-02 15:04", res.DateString+" "+res.TimeString); err != nil {
			HandleError(c, err)
		} else {
			res.Date = d
		}

		res.Tags = strings.Split(res.TagString, ",")

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

		tx, err := db.Begin()
		if err != nil {
			HandleError(c, err)
			return
		}
		loc := "./post/" + res.URI
		switch res.TransactionType {
		case "DELETE":
			err = DeleteContent(tx, &res.ContentPiece)
			loc = "./"
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
			c.JSON(201, res.ContentPiece)
			return
		}
		c.Redirect(302, loc)
	})

	// Alias "page/my-page" for assets directory file finding of "assets/my-page.html"
	r.GET("/page/:filename", func(c *gin.Context) {
		c.File(filepath.Join(assetsDir, "pages", c.Params.ByName("filename") + ".html"))
	})

	r.POST("/files", func(c *gin.Context) {
		if !IsAuthorized(c) {
			HandleError(c, ErrNoAuth)
			return
		}

		dir := c.PostForm("Directory")
		h, err := c.FormFile("File")
		if err != nil {
			HandleError(c, err)
			return
		}
		src, err := h.Open()
		if err != nil {
			HandleError(c, err)
			return
		}
		defer src.Close()
		if err := os.MkdirAll(path.Join(assetsDir, dir), 0755); err != nil {
			HandleError(c, err)
			return
		}
		dst, err := os.OpenFile(path.Join(assetsDir, dir, h.Filename), os.O_RDWR|os.O_TRUNC|os.O_CREATE,0755)
		if err != nil {
			HandleError(c, err)
			return
		}
		defer dst.Close()
		if _, err := io.Copy(dst, src); err != nil {
			HandleError(c, err)
			return
		}
		c.Redirect(302, filepath.Join("/files", dir))
	})

	r.GET("/files/*path", func(c *gin.Context) {
		p := c.Params.ByName("path")
		filename := path.Join(assetsDir, p)
		fi, err := os.Stat(filename)
		if err != nil {
			HandleError(c, err)
			return
		}
		if _, ok := c.GetQuery("delete"); ok {
			if err := os.RemoveAll(filename); err != nil {
				HandleError(c, err)
				return
			}
			c.HTML(200, "notice.html", map[string]string{
				"Message": fmt.Sprintf("Deleted file %s", p),
				"ReturnURL": filepath.Join("/files", filepath.Dir(p)),
			})
			return
		}
		if q, ok := c.GetQuery("size"); ok && IsImage(fi) {
			ServeImageCache(c, filename, q)
			return
		}
		if fi.IsDir() {
			if !IsAuthorized(c) {
				HandleError(c, ErrNoAuth)
				return
			}

			var files []FileItem
			if err := filepath.Walk(filename, func(path string, info os.FileInfo, err error) error {
				if path == filename {
					return nil
				}
				if err != nil {
					return nil
				}
				var f FileItem
				f.Path = filepath.Join(p, path[len(filename):])
				f.Filename = info.Name()
				f.IsDirectory = info.IsDir()
				files = append(files, f)
				return nil
			}); err != nil {
				HandleError(c, err)
				return
			}
			payload := M{
				"Directory": p,
				"Files":     files,
			}
			if IsReqJSON(c) {
				c.JSON(200, payload)
			} else {
				c.HTML(200, "files.html", payload)
			}
			return
		}
		c.File(filename)
	})

	if key != "" && cert != "" {
		r.RunTLS(":" + strconv.Itoa(port), cert, key)
	} else {
		r.Run(":" + strconv.Itoa(port))
	}
}

type FileItem struct {
	Filename    string
	Path        string
	URI         string
	IsDirectory bool
}

func HandleError(c *gin.Context, err error) {
	w := map[string]string{"Error": err.Error()}
	if _, ok := c.GetQuery("json"); ok {
		c.JSON(500, w)
		return
	}
	c.HTML(500, "error.html", w)
}

func IsReqJSON(c *gin.Context) bool {
	if _, ok := c.GetQuery("json"); ok {
		return true
	}
	return false
}

func IsImage(info os.FileInfo) bool {
	ext := strings.ToLower(filepath.Ext(info.Name()))
	return ext == ".jpg" || ext == ".jpeg" || ext == ".png"
}

func ServeImageCache(c *gin.Context, filename, size string) {
	s, err := strconv.Atoi(size)
	if err != nil {
		HandleError(c, errors.New(fmt.Sprintf("invalid image size '%s'", size)))
		return
	}
	dir := filepath.Dir(filename)
	base := filepath.Base(filename)
	ext := filepath.Ext(filename)
	name := base[:len(base)-len(ext)]
	cached := filepath.Join(dir, fmt.Sprintf("%s_%s_%s", name, size, ext))
	info, err := os.Stat(cached)
	if os.IsNotExist(err) {
		img, err := imaging.Open(filename)
		if err != nil {
			HandleError(c, err)
			return
		}
		img = imaging.Fit(img, s, s, imaging.Lanczos)
		if err := imaging.Save(img, cached); err != nil {
			HandleError(c, err)
			return
		}
		c.File(cached)
	} else if err != nil {
		HandleError(c, err)
		return
	} else if info.IsDir() {
		HandleError(c, errors.New("cached image file is a directory"))
		return
	}
	c.File(cached)
}