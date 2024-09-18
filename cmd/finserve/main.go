package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/deitrix/fin"
	"github.com/deitrix/fin/auth"
	"github.com/deitrix/fin/pkg/pointer"
	"github.com/deitrix/fin/store/file"
	"github.com/deitrix/fin/web/assets"
	"github.com/deitrix/fin/web/page"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rickb777/date"
)

type config struct {
	Auth auth.Config `json:"auth"`
}

func readConfig(path string) (config, error) {
	bs, err := os.ReadFile(path)
	if err != nil {
		return config{}, fmt.Errorf("reading config file: %w", err)
	}
	var c config
	if err := json.Unmarshal(bs, &c); err != nil {
		return config{}, fmt.Errorf("unmarshalling config file: %w", err)
	}
	return c, nil
}

func main() {
	slog.SetLogLoggerLevel(slog.LevelDebug)

	configPath := flag.String("config", "config.json", "path to config file")
	flag.Parse()
	if *configPath == "" {
		log.Fatal("config file path is required")
	}

	conf, err := readConfig(*configPath)
	if err != nil {
		log.Fatalf("reading config: %v", err)
	}

	store := file.NewStore("fin.json")

	router := chi.NewRouter()

	router.Use(auth.Verify(conf.Auth))

	router.Get("/assets/style.css", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, assets.FS, "style.css")
	})

	router.Get("/", func(w http.ResponseWriter, r *http.Request) {
		var email string
		profile, ok := auth.ProfileFromContext(r.Context())
		if ok {
			email = profile["email"].(string)
		}
		rps, err := store.RecurringPayments(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		payments := fin.PaymentsSinceN(rps, date.Today(), 11)
		var nextPayments *int
		if len(payments) == 11 {
			nextPayments = pointer.To(20)
			payments = payments[:10]
		}
		data := page.HomeData{
			RecurringPayments: rps,
			Payments:          payments,
			PaymentsState: page.PaymentsState{
				CurrentPage: 10,
			},
			NextPayments: nextPayments,
			Email:        email,
		}
		if q := r.URL.Query().Get("q"); q != "" {
			var filtered []fin.RecurringPayment
			for _, rp := range rps {
				if strings.Contains(strings.ToLower(rp.Name), strings.ToLower(q)) {
					filtered = append(filtered, rp)
				}
			}
			data.RecurringPayments = filtered
		}
		render(w, r, page.Home(data))
	})

	router.Get("/render/payments", func(w http.ResponseWriter, r *http.Request) {
		size, err := strconv.Atoi(r.URL.Query().Get("size"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		q := r.URL.Query().Get("q")
		rps, err := store.RecurringPayments(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		payments := fin.PaymentsSinceNFilter(rps, date.Today(), size+1, func(payment fin.Payment) bool {
			if q == "" {
				return true
			}
			return strings.Contains(strings.ToLower(payment.RecurringPayment.Name), strings.ToLower(q))
		})
		var nextPage *int
		if len(payments) == size+1 {
			nextPage = pointer.To(size + 10)
			payments = payments[:size]
		}
		render(w, r, page.Payments(payments, page.PaymentsState{
			CurrentPage: size,
			Query:       q,
		}, nextPage))
	})

	router.Get("/recurring-payments/{id}", func(w http.ResponseWriter, r *http.Request) {
		var email string
		profile, ok := auth.ProfileFromContext(r.Context())
		if ok {
			email = profile["email"].(string)
		}
		id := chi.URLParam(r, "id")
		rp, err := store.RecurringPayment(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		since := date.Today()
		if q := r.URL.Query().Get("since"); q != "" {
			var err error
			since, err = date.ParseISO(q)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		payments := rp.PaymentsSinceN(since, 6)
		var loadMoreSince *date.Date
		if len(payments) == 6 {
			loadMoreSince = &payments[5].Date
			payments = payments[:5]
		}
		render(w, r, page.RecurringPayment(email, rp, payments, loadMoreSince))
	})

	router.Get("/create", func(w http.ResponseWriter, r *http.Request) {
		var email string
		profile, ok := auth.ProfileFromContext(r.Context())
		if ok {
			email = profile["email"].(string)
		}
		render(w, r, page.RecurringPaymentCreate(email))
	})

	router.Post("/create", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		fmt.Println(r.Form.Get("debt"))
		rp := fin.RecurringPayment{
			ID:      uuid.NewString(),
			Name:    r.Form.Get("name"),
			Enabled: true,
			Debt:    r.Form.Get("debt") == "on",
		}
		if err := store.CreateRecurringPayment(r.Context(), rp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	log.Fatal(http.ListenAndServe(":8080", router))
}

func render(w http.ResponseWriter, r *http.Request, component templ.Component) {
	if err := component.Render(r.Context(), w); err != nil {
		slog.ErrorContext(r.Context(), "error rendering page", err)
	}
}
