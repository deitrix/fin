package handlers

import (
	"net/http"

	"github.com/deitrix/fin/ui"
	"github.com/deitrix/fin/ui/page"
)

func Home(w http.ResponseWriter, r *http.Request) {
	ui.Render(w, r, page.Home())
}
