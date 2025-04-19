package main

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"sort"
	"strconv"
	"time"

	"embed"

	"github.com/alecthomas/kingpin/v2"
	"github.com/vmihailenco/msgpack"
)

var (
	dbArg  = kingpin.Arg("db", "database file").Required().String()
	dbFile string

	db dbData
)

//go:embed index.html
var fs embed.FS

func main() {
	kingpin.Parse()
	dbFile = *dbArg

	if err := loadDb(); err != nil {
		fmt.Println(err)
		return
	}

	tmpl, err := template.ParseFS(fs, "index.html")
	if err != nil {
		fmt.Println(err)
		return
	}

	http.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if err := tmpl.Execute(w, db); err != nil {
			fmt.Println(err)
		}
	})

	http.HandleFunc("POST /", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			fmt.Println(err)
			return
		}

		form := r.FormValue("form")
		action := r.FormValue("action")

		switch form {
		case "days":
			switch action {
			case "save":
				var newDays []daysRow

				for i := range db.Days {
					fv := func(s string) string {
						return r.PostFormValue(fmt.Sprintf("%s_%d", s, i))
					}

					date := fv("date")
					hours, err := strconv.ParseFloat(fv("hours"), 64)
					if err != nil {
						fmt.Println(err)
					}
					paid := fv("paid") != ""
					del := fv("delete") != ""

					if del {
						continue
					}

					newDays = append(newDays, daysRow{
						Date:  date,
						Hours: hours,
						Pay:   0,
						Paid:  paid,
						Owed:  0,
					})
				}

				db.Days = newDays
			case "add":
				db.Days = append(db.Days, daysRow{
					Date:  time.Now().Format(time.DateOnly),
					Hours: 0,
					Pay:   0,
					Paid:  false,
					Owed:  0,
				})
			}
		case "rates":
			switch action {
			case "save":
				var newRates []ratesRow
				for i := range db.Rates {
					fv := func(s string) string {
						return r.PostFormValue(fmt.Sprintf("%s_%d", s, i))
					}

					date := fv("date")
					rate, err := strconv.ParseFloat(fv("rate"), 64)
					if err != nil {
						fmt.Println(err)
					}
					del := fv("delete") != ""

					if del {
						continue
					}

					newRates = append(newRates, ratesRow{
						Date: date,
						Rate: rate,
					})
				}

				db.Rates = newRates
			case "add":
				db.Rates = append(db.Rates, ratesRow{
					Date: time.Now().Format(time.DateOnly),
					Rate: 0,
				})
			}
		}

		recalculateDb()

		if err := saveDb(); err != nil {
			fmt.Println(err)
		}

		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	fmt.Println("http://127.0.0.1:8080/")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		fmt.Println(err)
		return
	}
}

type daysRow struct {
	Date  string
	Hours float64
	Pay   float64
	Paid  bool
	Owed  float64
}

type daysTotalRow struct {
	Hours float64
	Pay   float64
	Owed  float64
}

type ratesRow struct {
	Date string
	Rate float64
}

type dbData struct {
	Days      []daysRow
	DaysTotal daysTotalRow
	Rates     []ratesRow
}

func recalculateDb() {
	sort.Slice(db.Rates, func(i, j int) bool {
		ti, errI := time.Parse(time.DateOnly, db.Rates[i].Date)
		tj, errJ := time.Parse(time.DateOnly, db.Rates[j].Date)

		if errI != nil && errJ != nil {
			return false
		}
		if errI != nil {
			return false
		}
		if errJ != nil {
			return true
		}

		return ti.Before(tj)
	})

	sort.Slice(db.Days, func(i, j int) bool {
		ti, errI := time.Parse(time.DateOnly, db.Days[i].Date)
		tj, errJ := time.Parse(time.DateOnly, db.Days[j].Date)

		if errI != nil && errJ != nil {
			return false
		}
		if errI != nil {
			return false
		}
		if errJ != nil {
			return true
		}

		return ti.Before(tj)
	})

	var sumHours float64
	var sumPay float64
	var sumOwed float64

	for i, d := range db.Days {
		pay := getRateForDate(d.Date) * d.Hours
		db.Days[i].Pay = pay
		sumHours += d.Hours
		sumPay += pay
		if !d.Paid {
			db.Days[i].Owed = pay
			sumOwed += pay
		} else {
			db.Days[i].Owed = 0
		}
	}

	db.DaysTotal.Hours = sumHours
	db.DaysTotal.Pay = sumPay
	db.DaysTotal.Owed = sumOwed
}

func getRateForDate(date string) float64 {
	t, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return 0
	}

	var bestDate time.Time
	var rate float64

	for _, r := range db.Rates {
		rt, err := time.Parse(time.DateOnly, r.Date)
		if err != nil {
			continue
		}
		if rt.After(t) {
			continue
		}
		if bestDate.IsZero() || rt.After(bestDate) {
			bestDate = rt
			rate = r.Rate
		}
	}
	return rate
}

func saveDb() error {
	os.Rename(dbFile, dbFile+".bak")
	f, err := os.Create(dbFile)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := msgpack.NewEncoder(f)
	return enc.Encode(db)
}

func loadDb() error {
	f, err := os.Open(dbFile)
	if err != nil {
		return nil
	}
	defer f.Close()
	dec := msgpack.NewDecoder(f)
	return dec.Decode(&db)
}
