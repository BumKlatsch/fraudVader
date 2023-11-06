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
	"net/http"
	"regexp"
	"strings"
	"time"
)

const lenOfPreviewText = 60

type filterQuery struct {
	Search  string `form:"search"`
	Start   string `form:"start"`
	End     string `form:"end"`
	Page    int    `form:"page"`
	isFraud bool
}

func main() {
	db, err := sql.Open("sqlite3", "./main.db")
	if err != nil {
		panic("cannot connect to database")
	}

	r := gin.Default()
	r.StaticFile("/favicon.ico", "favicon.ico")
	r.GET("/", func(ctx *gin.Context) {
		var filter filterQuery
		err := ctx.ShouldBindQuery(&filter)
		if err != nil {
			// TODO: logging
		}
		if filter.Page == 0 {
			filter.Page = 1
		}
		filter.isFraud = true

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
			"Mails":          mails,
			"Filter":         filter,
			"FilterInfoText": buildFilterInfoText(filter),
		})
		if err != nil {
			log.Println(err)
		}
	})

	r.GET("/graph-data", func(ctx *gin.Context) {
		var filter filterQuery
		err := ctx.ShouldBindQuery(&filter)
		if err != nil {
			// TODO: logging
		}

		dates, err := queryGraphData(db, filter)
		if err != nil {
			// TODO: do logging
		}

		ctx.JSON(http.StatusOK, gin.H{
			"data": createGraphDataFromMails(dates),
		})

	})
	r.Run("127.0.0.1:8080")
}

type GraphData struct {
	Weekdays      [7]int `json:"weekdays"`
	WeekdaysFraud [7]int `json:"weekdays_fraud"`
	Hours         [4]int `json:"hours"`
	HoursFraud    [4]int `json:"hours_fraud"`
}

type MailData struct {
	MessageID  string
	Hash       string
	Date       time.Time
	From       string
	To         []string
	Subject    string
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

	query := fmt.Sprintf("select message_id, `date`, `from`, `to`, subject, text, terms, terms_count from mails %s order by date desc limit %d offset %d", whereCondition, limit, offset)
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
		var subjectString *string
		var err = rows.Scan(&mail.MessageID, &dateString, &mail.From, &receivers, &subjectString, &textString, &termsString, &mail.TermsCount)
		if err != nil {
			return nil, err
		}

		foundTerms := regexp.MustCompile(`'.+'`).
			FindAllString(termsString, -1)
		mail.Terms = foundTerms

		foundMails := regexp.MustCompile(`[\w-\.]+@([\w-]+\.)+[\w-]{2,4}`).
			FindAllString(receivers, -1)
		mail.To = foundMails

		dateString = strings.ReplaceAll(dateString, "+00:00", "")
		t, err := time.Parse("2006-01-02 15:04:05", dateString)
		if err != nil {
			return nil, err
		}
		mail.Date = t

		if textString != nil {
			mail.Text = *textString
			previewTextLen := math.Min(float64(len(mail.Text)), float64(lenOfPreviewText))
			mail.Summary = mail.Text[:int(previewTextLen)]
		}

		if subjectString != nil {
			mail.Subject = *subjectString
		}

		hash := sha256.Sum256([]byte(mail.MessageID))
		mail.Hash = base64.RawURLEncoding.EncodeToString(hash[:])
		output = append(output, mail)
	}

	return output, nil
}

func queryGraphData(db *sql.DB, filter filterQuery) (map[bool][]time.Time, error) {
	filterAddOns := buildQueryConditions(filter)
	whereCondition := ""
	if filterAddOns != "" {
		whereCondition = "where " + filterAddOns
	}

	query := "select `date`, terms_count > 0 from mails " + whereCondition
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}

	output := map[bool][]time.Time{
		false: {},
		true:  {},
	}
	for rows.Next() {
		var dateString string
		var isFraud bool
		err = rows.Scan(&dateString, &isFraud)
		if err != nil {
			return nil, err
		}
		t, err := time.Parse(time.DateTime+"+00:00", dateString)
		if err != nil {
			return nil, err
		}

		output[isFraud] = append(output[isFraud], t)
	}

	return output, nil
}

func buildQueryConditions(filter filterQuery) string {
	var added []string

	if filter.isFraud {
		added = append(added, "terms_count > 0")
	}
	if filter.Start != "" {
		added = append(added, fmt.Sprintf("date > '%s'", filter.Start))
	}
	if filter.End != "" {
		added = append(added, fmt.Sprintf("date < '%s'", filter.End))
	}
	if filter.Search != "" {
		added = append(added, fmt.Sprintf("(text like '%%%s%%' OR `to` like '%%%s%%' OR `from` like '%%%s%%')", filter.Search, filter.Search, filter.Search))
	}

	return strings.Join(added, " AND ")
}

func buildFilterInfoText(f filterQuery) string {
	parts := make([]string, 0)

	if f.Search != "" {
		parts = append(parts, "Suche nach: "+f.Search)
	}

	if f.Start != "" {
		t, err := time.Parse("2006-01-02", f.Start)
		if err == nil {
			parts = append(parts, "von: "+t.Format("02.01.2006"))
		}
	}

	if f.End != "" {
		t, err := time.Parse("2006-01-02", f.End)
		if err == nil {
			parts = append(parts, "bis: "+t.Format("02.01.2006"))
		}
	}

	return strings.Join(parts, " | ")
}

func createGraphDataFromMails(data map[bool][]time.Time) GraphData {
	weekdays := make(map[time.Weekday]int, 0)
	weekdaysFraud := make(map[time.Weekday]int, 0)
	hours := make(map[int]int, 0)
	hoursFraud := make(map[int]int, 0)

	for isFraud, dates := range data {
		for _, date := range dates {
			weekday := date.Weekday()
			if _, ok := weekdays[weekday]; !ok {
				weekdays[weekday] = 0
			}
			if isFraud {
				weekdaysFraud[weekday] += 1
			} else {
				weekdays[weekday] += 1
			}

			hour := date.Hour()
			if _, ok := hours[hour]; !ok {
				hours[hour] = 0
			}
			if isFraud {
				hoursFraud[hour] += 1
			} else {
				hours[hour] += 1
			}
		}
	}

	var weekdaysOutput [7]int
	var weekdaysOutputFraud [7]int
	weekdaysOutput[6] = weekdays[time.Sunday]
	weekdaysOutputFraud[6] = weekdaysFraud[time.Sunday]
	for i := 1; i < 7; i++ {
		// because we want to start at monday, we start a 1
		weekdaysOutput[i-1] = weekdays[time.Weekday(i)]
		weekdaysOutputFraud[i-1] = weekdaysFraud[time.Weekday(i)]
	}

	var hoursOutput [4]int
	var hoursOutputFraud [4]int
	for hour, value := range hours {
		switch hour {
		case 0, 1, 2, 3, 4, 5:
			hoursOutput[0] += value
			hoursOutputFraud[0] += value
		case 6, 7, 8, 9, 10, 11:
			hoursOutput[1] += value
			hoursOutputFraud[1] += value
		case 12, 13, 14, 15, 16, 17:
			hoursOutput[2] += value
			hoursOutputFraud[2] += value
		case 18, 19, 20, 21, 22, 23:
			hoursOutput[3] += value
			hoursOutputFraud[3] += value
		default:
			panic("WHOOOPSIE. Hour is strange")
		}
	}

	return GraphData{
		Weekdays:      weekdaysOutput,
		WeekdaysFraud: weekdaysOutputFraud,
		Hours:         hoursOutput,
		HoursFraud:    hoursOutputFraud,
	}
}
