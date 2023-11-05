package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"html/template"
	"log"
	"math"
	"regexp"
	"strings"
	"time"
)

const lenOfPreviewText = 60

type filterQuery struct {
	Search string `form:"search"`
	Start  string `form:"start"`
	End    string `form:"end"`
	Page   int    `form:"page"`
}

func main() {
	db, err := sql.Open("sqlite3", "./main.db")
	if err != nil {
		panic("cannot connect to database")
	}

	r := gin.Default()
	r.GET("/", func(ctx *gin.Context) {
		var filter filterQuery
		err := ctx.ShouldBindQuery(&filter)
		if err != nil {
			// TODO: logging
		}
		if filter.Page == 0 {
			filter.Page = 1
		}

		funcs := template.FuncMap{
			"inc": func(value int) int {
				return value + 1
			},
			"dec": func(value int) int {
				return value - 1
			},
		}

		tmpl, err := template.New("view.html").Funcs(funcs).ParseFiles("./view.html")
		if err != nil {
			// TODO: do logging
		}

		mails, err := queryData(db, filter)
		if err != nil {
			panic(err)
		}

		if mails == nil {
			mails = make([]MailData, 0)
		}

		err = tmpl.Execute(ctx.Writer, gin.H{
			"Mails":  mails,
			"Filter": filter,
		})
		if err != nil {
			log.Println(err)
		}
	})
	r.Run("127.0.0.1:8080")
}

type MailData struct {
	MessageID  string
	Hash       string
	Date       time.Time
	From       string
	To         []string
	Subject    *string
	Text       string
	Terms      []string
	TermsCount int
	Summary    string
}

func queryData(db *sql.DB, filter filterQuery) ([]MailData, error) {
	limit := 50
	offset := (filter.Page - 1) * limit

	filterAddOns := buildQueryConditions(filter)
	whereCondition := ""
	if filterAddOns != "" {
		whereCondition = "where " + filterAddOns
	}

	query := fmt.Sprintf("select message_id, `date`, `from`, `to`, subject, text, terms, terms_count from mails %s limit %d offset %d", whereCondition, limit, offset)
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}

	var output []MailData
	for rows.Next() {
		var mail MailData
		var dateString string
		var termsString string
		var textString *string
		var receivers string
		var err = rows.Scan(&mail.MessageID, &dateString, &mail.From, &receivers, &mail.Subject, &textString, &termsString, &mail.TermsCount)
		if err != nil {
			return nil, err
		}

		foundTerms := regexp.MustCompile(`'.+'`).
			FindAllString(termsString, -1)
		mail.Terms = foundTerms

		foundMails := regexp.MustCompile(`[\w-\.]+@([\w-]+\.)+[\w-]{2,4}`).
			FindAllString(receivers, -1)
		mail.To = foundMails

		t, err := parseDate(dateString)
		if err != nil {
			return nil, err
		}
		mail.Date = t
		if textString != nil {
			mail.Text = *textString
			previewTextLen := math.Min(float64(len(mail.Text)), float64(lenOfPreviewText))
			mail.Summary = mail.Text[:int(previewTextLen)]
		}

		hash := sha256.Sum256([]byte(mail.MessageID))
		mail.Hash = base64.RawURLEncoding.EncodeToString(hash[:])
		output = append(output, mail)
	}

	return output, nil
}

func buildQueryConditions(filter filterQuery) string {
	var added []string
	if filter.Start != "" {
		// added = append(added, fmt.Sprintf("date > %s", filter.Start))
	}
	if filter.End != "" {
		// added = append(added, fmt.Sprintf("date < %s", filter.End))
	}
	if filter.Search != "" {
		added = append(added, fmt.Sprintf("(text like '%%%s%%' OR `to` like '%%%s%%' OR `from` like '%%%s%%')", filter.Search, filter.Search, filter.Search))
	}

	return strings.Join(added, " AND ")
}

func parseDate(s string) (time.Time, error) {
	layouts := []string{
		"Mon, 02 Jan 2006 15:04:05 -0700 (MST)",
		"Mon, 2 Jan 2006 15:04:05 -0700 (MST)",
		"02 Jan 2006 15:04:05 -0700 (MST)",
		"2 Jan 2006 15:04:05 -0700 (MST)",
	}

	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("cannot convert time %q", s)
}
