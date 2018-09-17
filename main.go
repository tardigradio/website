package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"regexp"

	"github.com/julienschmidt/httprouter"
)

func Index(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if err := renderByPath(w, "./views/index.html"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func renderByPath(w http.ResponseWriter, path string) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	tmpl, err := template.ParseFiles(path)
	if err != nil {
		return err
	}

	tmpl.Execute(w, "")
	return nil
}

func main() {
	port := "8080"

	if len(os.Args) > 1 {
		if matched, _ := regexp.MatchString(`^\d{2,6}$`, os.Args[1]); matched == true {
			port = os.Args[1]
		}
	}

	fmt.Printf("Starting server at port %s...\n", port)

	router := httprouter.New()
	router.GET("/", Index)
	log.Fatal(http.ListenAndServe(":"+port, router))
}
