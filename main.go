package main

import (
	"net/http"

	"github.com/gorilla/mux"
)

var router = mux.NewRouter().StrictSlash(true)

func main() {

	go orbit.start()

	router.HandleFunc("/", cWs)

	http.Handle("/", router)
	http.ListenAndServe(":1337", router)
}
