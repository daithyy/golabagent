package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/daithyy/golabagent/api"
	"github.com/daithyy/golabagent/cfg"
)

const version = "v0.0.0"

func main() {
	showHelp := flag.Bool("help", false, "show help information")
	showVersion := flag.Bool("version", false, "show version information")

	flag.Usage = func() {
		log.Printf("usage: %s [options]\n", os.Args[0])
		log.Println("\ngolabagent - Lab Insight Agent")
		log.Println("\noptions:")
		flag.PrintDefaults()
		log.Println("\nexamples:")
		log.Printf("  %s -help                     # Show this help\n", os.Args[0])
	}

	flag.Parse()

	if *showHelp {
		flag.Usage()
		os.Exit(0)
	}

	if *showVersion {
		log.Printf("golabagent %s\n", version)
		log.Println("Lab Insight Agent")
		os.Exit(0)
	}

	if err := cfg.Parse(); err != nil {
		log.Fatal("error parsing .env file")
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		log.Println("shutting down gracefully...")
		// cleanup
		os.Exit(0)
	}()

	router := api.Setup()

	if err := router.Run(":" + cfg.Config.ServerPort); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}
