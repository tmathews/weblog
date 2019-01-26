# Tom's Blog

Tom's blog is a simple stand alone piece of software meant to be used for 
recording and sharing your thoughts. It uses Sqlite3 and the local file system 
to store it's contents.

## Features

 * Simple login system
 * Image thumbnailing
 * WYSIWYG editor for HTML posts
 * Reposting & liking URLs
 * URL Oembed previews
 * Simple file manager
 * No external database
 * Post scheduling & previewing
 * Cross platform! (Thanks Golang)
 * Simple HTML templating system
 * Page aliasing for html files
 * JSON API
 
## The Goal

The goal of this project was to build a simple solution to replace 
centralized platforms - such as Wordpress, Twitter, Tumblr, etc. - with an easy 
host and deploy it yourself method. Using Oembed and OpenGraph we can use the
 web's natural decentralization to reference other content and preview it.
 
## How To Use

### Getting Started

Use the help flag, `-h` to see the available flags to start weblog.

### Image Thumbnailing

Simply add the query parameter `size` with a valid integer to resize the target
JPG or PNG image. e.g. `/files/me.jpeg?size=256`

### WYSIWYG Editor

By default without JavaScript on the you can edit your post's HTML through a
textarea. The default templates includes the 
[Pell Editor](https://github.com/jaredreich/pell) with a few extras. You can 
easily replace this with your own in the templates.

### Templating

You can customize your blog to your hearts content. Please read more about
[Golang's templating system](https://golang.org/pkg/text/template/).

**Required Templates**

 * post.html
 * all.html
 * editor.html
 * error.html
 * files.html
 * login.html
 * notice.html

### JSON API

You can add the query parameter `json` to the index and post pages to get the
JSON version of the results. Updating content still requires requests made with
HTTP form payloads and does not accept JSON.

### File System

You can upload and delete files to the system by logging in and visiting the
`/files` page.