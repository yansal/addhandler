package main

import (
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"os/exec"
	"path/filepath"
	"plugin"
	"sync"

	"github.com/pkg/errors"
)

type store struct {
	sync.Mutex
	Data map[string]string
}

func main() {
	log.SetFlags(0)

	store := &store{Data: make(map[string]string)}

	// TODO: embed templates dir
	templates, err := template.New("").ParseGlob("templates/*.html")
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", rootHandler(store, templates))
	mux.Handle("/favicon.ico", http.NotFoundHandler())
	mux.Handle("/addhandler", addhandler(store, mux))

	s := http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	log.Fatal(s.ListenAndServe())
}

type handlerFunc func(w http.ResponseWriter, r *http.Request) error

func handleError(h handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			http.Error(w, fmt.Sprintf("%+v", err), http.StatusInternalServerError)
		}
	}
}

func rootHandler(store *store, template *template.Template) http.HandlerFunc {
	return handleError(func(w http.ResponseWriter, r *http.Request) error {
		store.Lock()
		data := store.Data
		store.Unlock()

		return errors.WithStack(template.ExecuteTemplate(w, "index.html", data))
	})
}

func addhandler(store *store, mux *http.ServeMux) http.HandlerFunc {
	return handleError(func(w http.ResponseWriter, r *http.Request) error {
		program := r.FormValue("program")

		// build
		path, err := build([]byte(program))
		if err != nil {
			return err
		}

		// load
		p, err := plugin.Open(path)
		if err != nil {
			return errors.WithStack(err)
		}
		sym, err := p.Lookup("H")
		if err != nil {
			return errors.WithStack(err)
		}
		h, ok := sym.(func(w http.ResponseWriter, r *http.Request))
		if !ok {
			return errors.Errorf("H has type %T, expected func(w http.ResponseWriter, r *http.Request)", sym)
		}

		// add handler and redirect
		mux.HandleFunc(path, h)

		store.Lock()
		store.Data[path] = program
		store.Unlock()

		http.Redirect(w, r, path, http.StatusFound)
		return nil
	})
}

func build(program []byte) (path string, err error) {
	// write
	dir, err := ioutil.TempDir("", "addhandler")
	if err != nil {
		return "", errors.WithStack(err)
	}
	path = filepath.Join(dir, "main.go")
	if err := ioutil.WriteFile(path, program, 0666); err != nil {
		return "", errors.WithStack(err)
	}

	// build
	cmd := exec.Command("go", "build", "-buildmode=plugin", "-o", "plugin.so")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if _, ok := err.(*exec.ExitError); ok {
		return "", errors.New(string(output))
	} else if err != nil {
		return "", errors.WithStack(err)
	}

	return filepath.Join(dir, "plugin.so"), nil
}
