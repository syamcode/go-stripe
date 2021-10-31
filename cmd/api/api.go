package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
	"github.com/syamcode/go-stripe/internal/driver"
	"github.com/syamcode/go-stripe/internal/models"
)

const version = "1.0.0"

type config struct {
	port int
	env  string
	db   struct {
		dsn string
	}
	stripe struct {
		secret string
		key    string
	}
	smtp struct {
		host     string
		port     int
		username string
		password string
	}
	secretkey string
	frontend  string
}

type application struct {
	config   config
	infoLog  *log.Logger
	errorLog *log.Logger
	version  string
	DB       models.DBModel
}

func (app *application) serve() error {
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", app.config.port),
		Handler:           app.routes(),
		IdleTimeout:       30 * time.Second,
		ReadTimeout:       10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      4 * time.Second,
	}

	app.infoLog.Printf("Starting Backend server in %s mode on port %d", app.config.env, app.config.port)

	return srv.ListenAndServe()
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	var cfg config

	flag.IntVar(&cfg.port, "port", 4001, "Server port to listen on")
	flag.StringVar(&cfg.env, "env", "development", "Application environment {development|production|maintenance}")
	flag.StringVar(&cfg.db.dsn, "dsn", "syamcode@tcp(localhost:3306)/widgets?parseTime=true&tls=false", "DSN")
	flag.StringVar(&cfg.frontend, "frontend", "http://localhost:4000", "url to frontend")

	flag.Parse()

	cfg.stripe.key = os.Getenv("STRIPE_KEY")
	cfg.stripe.secret = os.Getenv("STRIPE_SECRET")
	cfg.smtp.host = os.Getenv("SMTP_HOST")
	cfg.smtp.port, _ = strconv.Atoi(os.Getenv("SMTP_PORT"))
	cfg.smtp.username = os.Getenv("SMTP_USERNAME")
	cfg.smtp.password = os.Getenv("SMTP_PASSWORD")
	cfg.secretkey = os.Getenv("SECRET_KEY")

	infoLog := log.New(os.Stdout, "INFO\t", log.Ldate|log.Ltime)
	errorLog := log.New(os.Stdout, "ERROR\t", log.Ldate|log.Ltime|log.Lshortfile)

	conn, err := driver.OpenDB(cfg.db.dsn)
	if err != nil {
		errorLog.Fatal(err)
	}
	defer conn.Close()

	app := &application{
		config:   cfg,
		infoLog:  infoLog,
		errorLog: errorLog,
		version:  version,
		DB:       models.DBModel{DB: conn},
	}

	err = app.serve()
	if err != nil {
		app.errorLog.Println(err)
		log.Fatal(err)
	}
}
