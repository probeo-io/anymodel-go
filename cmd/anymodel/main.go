package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/probeo-io/anymodel-go/server"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		serveCmd := flag.NewFlagSet("serve", flag.ExitOnError)
		port := serveCmd.Int("port", 4141, "port to listen on")
		host := serveCmd.String("host", "0.0.0.0", "host to bind to")
		serveCmd.Parse(os.Args[2:])

		if err := server.Start(server.Options{
			Port: *port,
			Host: *host,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage: anymodel <command> [options]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  serve   Start the anymodel HTTP server")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  --port  Port to listen on (default: 4141)")
	fmt.Println("  --host  Host to bind to (default: 0.0.0.0)")
}
