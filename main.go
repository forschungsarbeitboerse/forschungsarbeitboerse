package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/gorilla/mux"
	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
	_ "github.com/mattn/go-sqlite3"
	"github.com/yuin/goldmark"
)

var (
	config          Config
	configPath      string
	db              *sql.DB
	sessionStore    *sessions.CookieStore
	validMailRegexp []*regexp.Regexp
	Version         string = "dev"
)

type Config struct {
	Addr string

	AdminEmail string `toml:"admin_email"`

	CookieSecret string `toml:"cookie_secret"`

	DBPath string

	TitleText  string `toml:"title_text"`
	FooterText string `toml:"footer_text"`
	InfoText   string `toml:"info_text"`

	PostingCategories []string `toml:"posting_categories"`
	PostingTypes      []string `toml:"posting_types"`

	SMTPHost     string `toml:"smtp_host"`
	SMTPMailFrom string `toml:"smtp_mail_from"`
	SMTPPass     string `toml:"smtp_pass"`
	SMTPPort     string `toml:"smtp_port"`
	SMTPUser     string `toml:"smtp_user"`

	URL string `toml:"url`

	ValidMailRegexp []string `toml:"valid_mail_regexp"`
}

func main() {
	var (
		err    error
		initDb bool
	)

	config.Addr = "127.0.0.1:8080"
	config.DBPath = "./forschungsarbeitboerse.sqlite3"

	flag.StringVar(&configPath, "config", "./forschungsarbeitboerse.toml", "path to config file")
	flag.BoolFunc("version", "print version and exit", func(s string) error {
		fmt.Printf("%s\n", Version)
		os.Exit(0)
		return nil
	})
	flag.BoolFunc("gen-cookie-secret", "print cookie secret and exit", func(s string) error {
		secret := securecookie.GenerateRandomKey(32)
		fmt.Printf("%s\n", fmt.Sprintf("%x", secret))
		os.Exit(0)
		return nil
	})
	flag.BoolVar(&initDb, "init-db", false, "initialize database and exit")

	flag.Parse()

	if _, err := os.Stat(configPath); err == nil {
		configData, err := os.ReadFile(configPath)
		if err != nil {
			log.Fatalf("failed to read config file: %v\n", err)
		}

		if _, err := toml.Decode(string(configData), &config); err != nil {
			log.Fatalf("failed to decode config: %v\n", err)
		}

		log.Printf("read config at: %s\n", configPath)
	} else if errors.Is(err, os.ErrNotExist) {
		log.Fatalf("failed to find config file at %q\n", configPath)
	} else {
		log.Fatalf("failed to stat config file %q: %v\n", configPath, err)
	}

	db, err = sql.Open("sqlite3", config.DBPath)
	if err != nil {
		log.Fatalf("failed to open database: %v\n", err)
	}

	if initDb {
		tmplInitSql := tmpl.Lookup("init.sql")
		if tmplInitSql == nil {
			log.Fatalf("failed to find init.sql in assets\n")
		}
		tmplBuf := new(bytes.Buffer)
		if err := tmplInitSql.Execute(tmplBuf, nil); err != nil {
			log.Fatalf("failed to execute init.sql template: %v\n", err)
		}
		if _, err := db.Exec(tmplBuf.String()); err != nil {
			log.Fatalf("failed to execute init database: %v\n", err)
		}
		log.Printf("initialized database at %q\n", config.DBPath)
		return

	}

	if config.CookieSecret == "" {
		log.Fatalf("cookie secret must be set\n")
	}

	cookieSecret, err := hex.DecodeString(config.CookieSecret)
	if err != nil {
		log.Fatalf("failed to decode cookie secret: %v\n", err)
	}

	sessionStore = sessions.NewCookieStore([]byte(cookieSecret))

	for _, v := range config.ValidMailRegexp {
		r, err := regexp.Compile(v)
		if err != nil {
			log.Fatalf("got invalid valid mail regular expression %q: %v\n", v, err)
		}
		validMailRegexp = append(validMailRegexp, r)
	}

	if _, err := db.Exec(`pragma journal_mode = WAL;`); err != nil {
		log.Fatalf("failed to set journal mode: %v\n", err)
	}
	if _, err := db.Exec(`pragma synchronous = normal;`); err != nil {
		log.Fatalf("failed to set synchronous mode: %v\n", err)
	}
	db.SetConnMaxLifetime(time.Second * 5)

	var buf bytes.Buffer
	if err := goldmark.Convert([]byte(config.InfoText), &buf); err != nil {
		log.Fatalf("failed to convert `info_text` markdown: %v\n", err)
	}
	config.InfoText = buf.String()

	buf.Reset()

	if err := goldmark.Convert([]byte(config.FooterText), &buf); err != nil {
		log.Fatalf("failed to convert `footer_text` markdown: %v\n", err)
	}
	config.FooterText = buf.String()

	r := mux.NewRouter()
	r.HandleFunc("/", handlerIndex).Methods("GET")
	r.HandleFunc("/new", handlerNew).Methods("GET", "POST")
	r.HandleFunc("/feed", handlerRSSFeed).Methods("GET")
	r.HandleFunc("/{uuid:[0-9A-Fa-f-]{36}}", handlerPosting).Methods("GET")
	r.HandleFunc("/{uuid:[0-9A-Fa-f-]{36}}/{token}/admin", handlerAdmin).Methods("GET", "POST")
	r.HandleFunc("/{uuid:[0-9A-Fa-f-]{36}}/{token}/preview", handlerPosting).Methods("GET")
	r.HandleFunc("/{uuid:[0-9A-Fa-f-]{36}}/{token}/verify", handlerVerify).Methods("GET")
	r.HandleFunc("/{uuid:[0-9A-Fa-f-]{36}}/{token}/delete", handlerDelete).Methods("POST")

	srv := &http.Server{
		Addr:         config.Addr,
		WriteTimeout: time.Second * 15,
		ReadTimeout:  time.Second * 15,
		IdleTimeout:  time.Second * 60,
		Handler:      r,
	}

	done := make(chan struct{})

	log.Printf("listening on %q\n", config.Addr)

	go func() {
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Println(err)
		}
	}()

	janitorTicker := time.NewTicker(time.Second * 30)
	go func() {
		for {
			select {
			case <-janitorTicker.C:
				janitorReverify()
			case <-done:
				log.Printf("janitor stopping\n")
				return
			}
		}
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	<-c

	log.Println("shutting down")

	close(done)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
	defer cancel()

	srv.Shutdown(ctx)

	log.Println("stopped")

	os.Exit(0)
}
