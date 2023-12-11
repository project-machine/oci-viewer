package main

import (
	"fmt"
	"log"
	"os"

	"github.com/urfave/cli/v2"
)

func main() {

	app := &cli.App{
		Name:      "ociv",
		Usage:     "interactively inspect oci layouts",
		Action:    doTViewStuff,
		ArgsUsage: "root dirs to inspect",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "registry",
				Aliases: []string{"r"},
				Usage:   "registry to fetch the tags",
			},
			&cli.StringFlag{
				Name:    "prefixes",
				Aliases: []string{"p"},
				Usage:   "comma separated repository prefixes to filter the tags",
				Value:   "", // Default is all prefixes
			},
		},
	}

	file, err := os.OpenFile("log.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatal(err)
	}

	log.SetOutput(file)

	if err := app.Run(os.Args); err != nil {
		fmt.Println(err)
		log.Fatal(err)
	}
}
