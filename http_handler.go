package main

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/smtp"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/feeds"
	"github.com/gorilla/mux"
)

type Posting struct {
	UUID      string
	CreatedAt time.Time

	Email string

	Title          string
	Institute      string
	Advisor        string
	Supervisor     string
	Audience       string
	Category       string
	Type           string
	Degree         string
	Start          string
	RequiredMonths int
	RequiredEffort string
	Text           string
}

type TemplateDataPage struct {
	// The page title text, shown in the upper left corner
	TitleText string

	// The page title, shown in the browser title bar
	PageTitle string

	// Flash messages and errors, if any
	FlashMessages []string
	FlashErrors   []string

	// The info text, shown on the index page
	InfoText template.HTML

	// The footer text, shown on all pages
	FooterText template.HTML

	// The build version, automatically set by the build system
	Version string
}

type TemplateDataIndex struct {
	TemplateDataPage

	Postings []Posting
}

type TemplateDataPosting struct {
	TemplateDataPage

	Posting
}

type TemplateDataForm struct {
	TemplateDataPage

	Posting

	Categories []string
	Institutes []string
	Types      []string

	AdminToken string

	// `IsEdit` is true if the form is used to edit an existing posting
	IsEdit bool
}

func handlerIndex(w http.ResponseWriter, r *http.Request) {
	session, err := sessionStore.Get(r, "s")
	if err != nil {
		log.Printf("error retrieving session: %v\n", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	var postings []Posting
	rows, err := db.Query(`
SELECT uuid, created_at, category, type, title, text
FROM postings
WHERE verified = 1
    AND deleted = 0
ORDER BY created_at DESC, id DESC`)
	if err != nil {
		log.Printf("error reading postings from database: %v\n", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	for rows.Next() {
		var p Posting
		if err := rows.Scan(&p.UUID, &p.CreatedAt, &p.Category, &p.Type, &p.Title, &p.Text); err != nil {
			log.Printf("error scanning posting: %v\n", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		postings = append(postings, p)
	}

	tmplData := TemplateDataIndex{
		TemplateDataPage: TemplateDataPage{
			PageTitle:  "Forschungsarbeitbörse",
			TitleText:  config.TitleText,
			InfoText:   template.HTML(config.InfoText),
			FooterText: template.HTML(config.FooterText),
			Version:    Version,
		},
		Postings: postings,
	}

	for _, flash := range session.Flashes() {
		tmplData.FlashMessages = append(tmplData.FlashMessages, flash.(string))
	}
	if err := session.Save(r, w); err != nil {
		log.Printf("error saving session: %v\n", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if err := tmpl.ExecuteTemplate(w, "index", tmplData); err != nil {
		log.Printf("error executing template: %v\n", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
}

func handlerNew(w http.ResponseWriter, r *http.Request) {
	session, err := sessionStore.Get(r, "s")
	if err != nil {
		log.Printf("error retrieving session: %v\n", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	tmplData := TemplateDataForm{
		TemplateDataPage: TemplateDataPage{
			PageTitle:  "Neues Angebot",
			TitleText:  config.TitleText,
			FooterText: template.HTML(config.FooterText),
			Version:    Version,
		},
		Categories: config.PostingCategories,
		Types:      config.PostingTypes,
	}

	if r.Method == "POST" {
		if err := r.ParseForm(); err != nil {
			log.Printf("error parsing form: %v\n", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		uuid := uuid.New().String()

		tmplData.Email = r.FormValue("email")
		tmplData.Title = r.FormValue("title")
		tmplData.Institute = r.FormValue("institute")
		tmplData.Advisor = r.FormValue("advisor")
		tmplData.Supervisor = r.FormValue("supervisor")
		tmplData.Audience = r.FormValue("audience")
		tmplData.Category = r.FormValue("category")
		tmplData.Type = r.FormValue("type")
		tmplData.Degree = r.FormValue("degree")
		tmplData.Start = r.FormValue("start")
		tmplData.RequiredMonths, err = strconv.Atoi(r.FormValue("required-months"))
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		tmplData.RequiredEffort = r.FormValue("required-effort")
		tmplData.Text = r.FormValue("text")

		// For postings from email addresses that are not on
		// the whitelist admins need to do the verification
		var requireAdminVerification = false

		if isForbiddenMailAddress(forbiddenMailRegexp, tmplData.Email) {
			log.Printf("attempt to create posting with forbidden mail address %q - rejecting\n", tmplData.Email)
			time.Sleep(5 * time.Second) // Be slow and hopefully a little annoying
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		if err := validateMailAddress(validMailRegexp, tmplData.Email); err != nil {
			if errors.Is(err, ErrUnknownEmail) {
				requireAdminVerification = true
			} else {
				tmplData.FlashErrors = append(tmplData.FlashErrors, fmt.Sprintf("Ungültige E-Mail Adresse (%q)", err.Error()))
			}
		}

		validateInput(&tmplData)

		if len(tmplData.FlashErrors) > 0 {
			goto EXEC_TMPL
		}

		admin_token, err := generateToken(30)
		if err != nil {
			log.Printf("error generating admin token: %v\n", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		verify_token, err := generateToken(30)
		if err != nil {
			log.Printf("error generating admin token: %v\n", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		_, err = db.Exec(`
INSERT INTO postings (
    uuid,
    email,
    admin_token,
    verify_token,
    title,
    institute,
    advisor,
    supervisor,
    audience,
    category,
    type,
    degree,
    start,
    required_months,
    required_effort,
    text
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			uuid, tmplData.Email, admin_token, verify_token, tmplData.Title, tmplData.Institute,
			tmplData.Advisor, tmplData.Supervisor, tmplData.Audience, tmplData.Category, tmplData.Type,
			tmplData.Degree, tmplData.Start, tmplData.RequiredMonths, tmplData.RequiredEffort, tmplData.Text)
		if err != nil {
			log.Printf("error inserting posting: %v\n", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		// Send admin and verification mails

		mailAuth := smtp.PlainAuth("", config.SMTPUser, config.SMTPPass, config.SMTPHost)
		mailFrom := config.SMTPMailFrom

		if requireAdminVerification {
			mailTo := []string{config.AdminEmail}

			mailTemplate := tmpl.Lookup("mail-admin.tmpl")
			if mailTemplate == nil {
				// This should not happen and be caught during testing
				log.Fatalf("error looking up mail template: %v\n", err)
			}

			mailText := new(bytes.Buffer)
			if err := mailTemplate.Execute(mailText, struct {
				To          string
				From        string
				UUID        string
				Title       string
				PreviewLink string
				VerifyLink  string
				AdminLink   string
			}{
				To:          tmplData.Email,
				From:        mailFrom,
				UUID:        uuid,
				Title:       tmplData.Title,
				PreviewLink: fmt.Sprintf("%s/%s/%s/preview", config.URL, uuid, admin_token),
				VerifyLink:  fmt.Sprintf("%s/%s/%s/verify", config.URL, uuid, verify_token),
				AdminLink:   fmt.Sprintf("%s/%s/%s/admin", config.URL, uuid, admin_token),
			}); err != nil {
				log.Printf("error executing mail template: %v\n", err)
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}

			if err := smtp.SendMail(fmt.Sprintf("%s:%s", config.SMTPHost, config.SMTPPort), mailAuth, mailFrom, mailTo, mailText.Bytes()); err != nil {
				log.Printf("error sending email: %v\n", err)
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
		}

		mailTo := []string{tmplData.Email}

		mailText := new(bytes.Buffer)
		if requireAdminVerification {
			mailTemplate := tmpl.Lookup("mail-user-unknown.tmpl")
			if mailTemplate == nil {
				// This should not happen and be caught during testing
				log.Fatalf("error looking up mail template: %v\n", err)
			}

			if err := mailTemplate.Execute(mailText, struct {
				To          string
				From        string
				UUID        string
				Title       string
				PreviewLink string
				VerifyLink  string
				AdminLink   string
			}{
				To:          tmplData.Email,
				From:        mailFrom,
				UUID:        uuid,
				Title:       tmplData.Title,
				PreviewLink: fmt.Sprintf("%s/%s/%s/preview", config.URL, uuid, admin_token),
				VerifyLink:  fmt.Sprintf("%s/%s/%s/verify", config.URL, uuid, verify_token),
				AdminLink:   fmt.Sprintf("%s/%s/%s/admin", config.URL, uuid, admin_token),
			}); err != nil {
				log.Printf("error executing mail template: %v\n", err)
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
		} else {
			mailTemplate := tmpl.Lookup("mail-user-whitelisted.tmpl")
			if mailTemplate == nil {
				// This should not happen and be caught during testing
				log.Fatalf("error looking up mail template: %v\n", err)
			}

			if err := mailTemplate.Execute(mailText, struct {
				To          string
				From        string
				UUID        string
				Title       string
				PreviewLink string
				VerifyLink  string
				AdminLink   string
			}{
				To:          tmplData.Email,
				From:        mailFrom,
				UUID:        uuid,
				Title:       tmplData.Title,
				PreviewLink: fmt.Sprintf("%s/%s/%s/preview", config.URL, uuid, admin_token),
				VerifyLink:  fmt.Sprintf("%s/%s/%s/verify", config.URL, uuid, verify_token),
				AdminLink:   fmt.Sprintf("%s/%s/%s/admin", config.URL, uuid, admin_token),
			}); err != nil {
				log.Printf("error executing mail template: %v\n", err)
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
		}

		mailAddr := fmt.Sprintf("%s:%s", config.SMTPHost, config.SMTPPort)
		if err := smtp.SendMail(mailAddr, mailAuth, mailFrom, mailTo, mailText.Bytes()); err != nil {
			log.Printf("error sending email: %v\n", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		flashMessage := "Angebot gespeichert. Zur Freischaltung bitte Verifizierungslink in E-Mail klicken."
		if requireAdminVerification {
			flashMessage = "Angebot gespeichert. Ihr Angebot wird in Kürze freigeschalten."
		}

		session.AddFlash(flashMessage)
		if err := session.Save(r, w); err != nil {
			log.Printf("error saving session: %v\n", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, config.URL, http.StatusFound)
		return
	}

EXEC_TMPL:

	institutes, err := listInstitutes()
	if err != nil {
		log.Printf("error reading institutes from database: %v\n", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	tmplData.Institutes = institutes

	if err := tmpl.ExecuteTemplate(w, "form", tmplData); err != nil {
		log.Printf("error executing template: %v\n", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

func handlerAdmin(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	uuid := vars["uuid"]
	token := vars["token"]

	tmplData := TemplateDataForm{
		TemplateDataPage: TemplateDataPage{
			TitleText:  config.TitleText,
			FooterText: template.HTML(config.FooterText),
			Version:    Version,
		},
		Categories: config.PostingCategories,
		Types:      config.PostingTypes,
		IsEdit:     true,
	}

	row := db.QueryRow(`
SELECT
    uuid,
    email,
    title,
    institute,
    advisor,
    supervisor,
    audience,
    category,
    type,
    degree,
    start,
    required_months,
    required_effort,
    text,
    admin_token
FROM postings
WHERE uuid = ?
    AND deleted = 0`,
		uuid)

	if err := row.Scan(
		&tmplData.UUID,
		&tmplData.Email,
		&tmplData.Title,
		&tmplData.Institute,
		&tmplData.Advisor,
		&tmplData.Supervisor,
		&tmplData.Audience,
		&tmplData.Category,
		&tmplData.Type,
		&tmplData.Degree,
		&tmplData.Start,
		&tmplData.RequiredMonths,
		&tmplData.RequiredEffort,
		&tmplData.Text,
		&tmplData.AdminToken); err != nil {
		if err == sql.ErrNoRows {
			handler404(w, r)
			return
		} else {
			log.Printf("error sql with uuid %q: %v\n", uuid, err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
	}

	if token != tmplData.AdminToken {
		log.Printf("got invalid admin token %q for uuid %q\n", token, uuid)
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}

	if r.Method == "POST" {
		if err := r.ParseForm(); err != nil {
			log.Printf("error parsing form: %v\n", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		var err error

		tmplData.Title = r.FormValue("title")
		tmplData.Institute = r.FormValue("institute")
		tmplData.Advisor = r.FormValue("advisor")
		tmplData.Supervisor = r.FormValue("supervisor")
		tmplData.Audience = r.FormValue("audience")
		tmplData.Category = r.FormValue("category")
		tmplData.Type = r.FormValue("type")
		tmplData.Degree = r.FormValue("degree")
		tmplData.Start = r.FormValue("start")
		tmplData.RequiredMonths, err = strconv.Atoi(r.FormValue("required-months"))
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		tmplData.RequiredEffort = r.FormValue("required-effort")
		tmplData.Text = r.FormValue("text")

		validateInput(&tmplData)

		if len(tmplData.FlashErrors) > 0 {
			goto EXEC_TMPL
		}

		_, err = db.Exec(`
UPDATE postings
SET
    title = ?,
    institute = ?,
    advisor = ?,
    supervisor = ?,
    audience = ?,
    category = ?,
    type = ?,
    degree = ?,
    start = ?,
    required_months = ?,
    required_effort = ?,
    text = ?,
    last_updated_at = CURRENT_TIMESTAMP
WHERE uuid = ?`,
			tmplData.Title, tmplData.Institute, tmplData.Advisor, tmplData.Supervisor, tmplData.Audience,
			tmplData.Category, tmplData.Type, tmplData.Degree, tmplData.Start, tmplData.RequiredMonths,
			tmplData.RequiredEffort, tmplData.Text, uuid)
		if err != nil {
			log.Printf("error updating posting with uuid %q: %v\n", uuid, err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, fmt.Sprintf("%s/%s", config.URL, uuid), http.StatusFound)
		return
	}

EXEC_TMPL:

	institutes, err := listInstitutes()
	if err != nil {
		log.Printf("error reading institutes from database: %v\n", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	tmplData.Institutes = institutes

	if err := tmpl.ExecuteTemplate(w, "form", tmplData); err != nil {
		log.Printf("error executing template: %v\n", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

func handlerPosting(w http.ResponseWriter, r *http.Request) {
	session, err := sessionStore.Get(r, "s")
	if err != nil {
		log.Printf("error retrieving session: %v\n", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	vars := mux.Vars(r)

	uuid := vars["uuid"]
	token := vars["token"]

	tmplData := TemplateDataPosting{
		TemplateDataPage: TemplateDataPage{
			TitleText:  config.TitleText,
			FooterText: template.HTML(config.FooterText),
			Version:    Version,
		},
		Posting: Posting{},
	}

	var (
		adminToken string
		verified   bool
	)

	row := db.QueryRow(`
SELECT
    created_at,
    email,
    title,
    institute,
    advisor,
    supervisor,
    audience,
    category,
    type,
    degree,
    start,
    required_months,
    required_effort,
    text,
    admin_token,
    verified
FROM postings
WHERE uuid = ?
    AND deleted = 0`,
		uuid)

	if err := row.Scan(
		&tmplData.CreatedAt,
		&tmplData.Email,
		&tmplData.Title,
		&tmplData.Institute,
		&tmplData.Advisor,
		&tmplData.Supervisor,
		&tmplData.Audience,
		&tmplData.Category,
		&tmplData.Type,
		&tmplData.Degree,
		&tmplData.Start,
		&tmplData.RequiredMonths,
		&tmplData.RequiredEffort,
		&tmplData.Text,
		&adminToken,
		&verified); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			handler404(w, r)
			return
		} else {
			log.Printf("error sql with uuid %q: %v\n", uuid, err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
	}

	if !verified {
		// This posting is not verified yet, only the admin can see it;
		// verify it's a valid admin token before showing the preview
		if token != adminToken {
			log.Printf("got invalid admin token %q for uuid %q\n", token, uuid)
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
	}

	tmplData.PageTitle = tmplData.Title

	for _, flash := range session.Flashes() {
		tmplData.FlashMessages = append(tmplData.FlashMessages, flash.(string))
	}
	if err := session.Save(r, w); err != nil {
		log.Printf("error saving session: %v\n", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if err := tmpl.ExecuteTemplate(w, "posting", tmplData); err != nil {
		log.Printf("error executing template: %v\n", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

func handlerRSSFeed(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	feed := &feeds.Feed{
		Title:   config.TitleText,
		Link:    &feeds.Link{Href: config.URL + "/feed"},
		Created: now,
	}

	rows, err := db.Query(`
SELECT uuid, created_at, category, title
FROM postings
WHERE verified = 1
    AND deleted = 0
ORDER BY created_at DESC, id DESC LIMIT 30`)
	if err != nil {
		log.Printf("error reading postings from database: %v\n", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	var postings []Posting
	for rows.Next() {
		var p Posting
		if err := rows.Scan(&p.UUID, &p.CreatedAt, &p.Category, &p.Title); err != nil {
			log.Printf("error scanning posting: %v\n", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		postings = append(postings, p)
	}

	for _, p := range postings {
		feed.Items = append(feed.Items, &feeds.Item{
			Title:   p.Title,
			Id:      p.UUID,
			Link:    &feeds.Link{Href: config.URL + "/" + p.UUID},
			Created: p.CreatedAt,
		})
	}

	rss, err := feed.ToRss()
	if err != nil {
		log.Printf("error generating RSS feed: %v\n", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/rss+xml")
	w.Write([]byte(rss))
}

func handlerVerify(w http.ResponseWriter, r *http.Request) {
	session, err := sessionStore.Get(r, "s")
	if err != nil {
		log.Printf("error retrieving session: %v\n", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	vars := mux.Vars(r)

	uuid := vars["uuid"]
	token := vars["token"]

	var verifyToken string

	row := db.QueryRow("SELECT verify_token FROM postings WHERE uuid = ?", uuid)

	if err := row.Scan(&verifyToken); err != nil {
		if err == sql.ErrNoRows {
			handler404(w, r)
			return
		} else {
			log.Printf("error sql with uuid %q: %v\n", uuid, err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
	}

	if token != verifyToken {
		log.Printf("got invalid verify token %q for uuid %q\n", token, uuid)
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}

	_, err = db.Exec("UPDATE postings SET verified = 1, last_verified_at = CURRENT_TIMESTAMP WHERE uuid = ? AND verify_token = ?", uuid, verifyToken)
	if err != nil {
		log.Printf("error verifying posting with uuid %q: %v\n", uuid, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	session.AddFlash("Angebot freigeschalten.")
	if err := session.Save(r, w); err != nil {
		log.Printf("error saving session: %v\n", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("%s/%s", config.URL, uuid), http.StatusFound)
}

func handlerDelete(w http.ResponseWriter, r *http.Request) {
	session, err := sessionStore.Get(r, "s")
	if err != nil {
		log.Printf("error retrieving session: %v\n", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	vars := mux.Vars(r)

	uuid := vars["uuid"]
	token := vars["token"]

	var adminToken string

	row := db.QueryRow("SELECT admin_token FROM postings WHERE uuid = ?", uuid)

	if err := row.Scan(&adminToken); err != nil {
		if err == sql.ErrNoRows {
			handler404(w, r)
			return
		} else {
			log.Printf("error sql with uuid %q: %v\n", uuid, err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
	}

	if token != adminToken {
		log.Printf("got invalid admin token %q for uuid %q\n", token, uuid)
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}

	_, err = db.Exec("UPDATE postings SET deleted = 1 WHERE uuid = ? AND admin_token = ?", uuid, adminToken)
	if err != nil {
		log.Printf("error soft deleting posting with uuid %q: %v\n", uuid, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	session.AddFlash("Angebot gelöscht.")
	if err := session.Save(r, w); err != nil {
		log.Printf("error saving session: %v\n", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, config.URL, http.StatusFound)
}

func handler404(w http.ResponseWriter, _ *http.Request) {
	tmplData := TemplateDataPosting{
		TemplateDataPage: TemplateDataPage{
			TitleText:  config.TitleText,
			FooterText: template.HTML(config.FooterText),
			Version:    Version,
		},
	}

	w.WriteHeader(http.StatusNotFound)
	if err := tmpl.ExecuteTemplate(w, "error404", tmplData); err != nil {
		log.Printf("error executing template: %v\n", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

func listInstitutes() ([]string, error) {
	var institutes []string

	rows, err := db.Query("SELECT DISTINCT institute FROM postings WHERE verified = 1 AND deleted = 0 ORDER BY id DESC")
	if err != nil {
		return nil, err
	}

	for rows.Next() {
		var institute string
		if err := rows.Scan(&institute); err != nil {
			return nil, err
		}
		institutes = append(institutes, institute)
	}

	return institutes, nil
}

func validateInput(tmplData *TemplateDataForm) {
	if tmplData.Title == "" {
		tmplData.FlashErrors = append(tmplData.FlashErrors, "Ein Titel ist erforderlich.")
	} else if len(tmplData.Title) > 500 {
		tmplData.FlashErrors = append(tmplData.FlashErrors, "Der \"Titel\" darf maximal 500 Zeichen lang sein.")
	}

	if len(tmplData.Institute) > 500 {
		tmplData.FlashErrors = append(tmplData.FlashErrors, "Das Angabe \"Institut\" darf maximal 500 Zeichen lang sein.")
	}

	if len(tmplData.Advisor) > 500 {
		tmplData.FlashErrors = append(tmplData.FlashErrors, "Die Angabe \"Betreuerin / Betreuer\" darf maximal 500 Zeichen lang sein.")
	}

	if len(tmplData.Supervisor) > 500 {
		tmplData.FlashErrors = append(tmplData.FlashErrors, "Die Angabe \"Doktormutter / Doktorvater\" darf maximal 500 Zeichen lang sein.")
	}

	if len(tmplData.Audience) > 500 {
		tmplData.FlashErrors = append(tmplData.FlashErrors, "Die Angabe \"Für Studierende der Fächer ...\" darf maximal 500 Zeichen lang sein.")
	}

	if len(tmplData.Category) > 500 {
		tmplData.FlashErrors = append(tmplData.FlashErrors, "Die Angabe \"Art\" darf maximal 500 Zeichen lang sein.")
	}

	if len(tmplData.Type) > 500 {
		tmplData.FlashErrors = append(tmplData.FlashErrors, "Die Angabe \"Typ\" darf maximal 500 Zeichen lang sein.")
	}

	if len(tmplData.Degree) > 500 {
		tmplData.FlashErrors = append(tmplData.FlashErrors, "Die Angabe \"Abschluss\" darf maximal 500 Zeichen lang sein.")
	}

	if len(tmplData.Start) > 500 {
		tmplData.FlashErrors = append(tmplData.FlashErrors, "Die Angabe \"Start\" darf maximal 500 Zeichen lang sein.")
	}

	if tmplData.RequiredMonths < 0 || tmplData.RequiredMonths > 120 {
		tmplData.FlashErrors = append(tmplData.FlashErrors, "Die Angabe \"Voraussichtliche Dauer in Monaten\" muss zwischen 0 und 120 Monaten liegen.")
	}

	if len(tmplData.RequiredEffort) > 500 {
		tmplData.FlashErrors = append(tmplData.FlashErrors, "Die Angabe \"Ungefährer Arbeitsaufwand\" darf maximal 500 Zeichen lang sein.")
	}

	if tmplData.Text == "" {
		tmplData.FlashErrors = append(tmplData.FlashErrors, "Eine Beschreibung ist erforderlich.")
	} else if len(tmplData.Text) > 10000 {
		tmplData.FlashErrors = append(tmplData.FlashErrors, "Die \"Beschreibung\" darf maximal 10000 Zeichen lang sein.")
	}
}
