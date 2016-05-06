package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/codegangsta/negroni"
	"github.com/gorilla/mux"
)

func main() {
	port := flag.Int("port", 0, "port")
	flag.Parse()
	if *port == 0 {
		log.Fatalln("port must be set")
	}

	r := mux.NewRouter()
	n := negroni.Classic()
	n.UseHandler(r)
	n.Run(fmt.Sprintf(":%d", *port))
}
