package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/MaMissaoui/Finglass/internal/core/i18n"
)

func main() {
	out := flag.String("out", "", "directory for the generated frontend files")
	flag.Parse()
	if *out == "" {
		fmt.Fprintln(os.Stderr, "i18ngen: -out is required")
		os.Exit(2)
	}
	if err := i18n.WriteFrontend(*out); err != nil {
		fmt.Fprintf(os.Stderr, "i18ngen: %v\n", err)
		os.Exit(1)
	}
}
