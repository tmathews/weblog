package main

import (
	"database/sql"
	"flag"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	var password string
	var dbfile string
	var sampleme bool
	var templateGlob string
	var assetsDir string
	var port int

	flag.IntVar(&port, "port", 8080, "Network port to occupy.")
	flag.StringVar(&password, "password", "password", "The password to validate editing.")
	flag.StringVar(&dbfile, "dbfile", "./a.db", "The database file to use for SQLite3.")
	flag.StringVar(&templateGlob, "templates", "./templates/*.html", "The template glob to use.")
	flag.StringVar(&assetsDir, "files", "./files", "Assets directory to serve.")
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

	StartServer(db, port, templateGlob, assetsDir, password)
}
