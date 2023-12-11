package main

import (
	"context"

	"github.com/k0kubun/go-ansi"
	"github.com/schollz/progressbar/v3"
	"github.com/urfave/cli/v2"
)

func doFetchTags(c *cli.Context) error {
	registry := c.String("registry")
	if registry == "" {
		return nil // We are not required to fetch flags.
	}

	prefixes := c.String("prefixes")
	out := getKnowLayersFilename()
	makeCacheDir() // Ensure the cache directory exists.

	reg := NewReg(registry, prefixes)
	ctx := context.Background()
	l, e := reg.GetRepoList(ctx)
	if e != nil {
		return e
	}

	entries := LayerNameHashEntries{}
	repos := []*Repo{}
	bar := progressbar.NewOptions(len(l.Repositories),
		progressbar.OptionSetWriter(ansi.NewAnsiStdout()),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionShowBytes(true),
		progressbar.OptionSetWidth(15),
		progressbar.OptionSetDescription("[cyan][1/2][reset] Fetching Repositories"),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "[green]=[reset]",
			SaucerHead:    "[green]>[reset]",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}))

	repoChan := make(chan RepoError, 16)
	go reg.FetchRepoInfos(l, repoChan)

	for repo := range repoChan {
		r, e := repo.Repo, repo.Err
		if e != nil {
			return e
		}
		if e := bar.Add(1); e != nil {
			return e
		}
		repos = append(repos, r)
	}

	totalTags := 0
	for _, r := range repos {
		totalTags += len(r.Tags)
	}

	bar = progressbar.NewOptions(totalTags,
		progressbar.OptionSetWriter(ansi.NewAnsiStdout()),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionShowBytes(true),
		progressbar.OptionSetWidth(15),
		progressbar.OptionSetDescription("[cyan][2/2][reset] Fetching Image Tags"),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "[green]=[reset]",
			SaucerHead:    "[green]>[reset]",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}))

	tagChan := make(chan LayerEntryError, 64)
	go reg.FetchLayerEntries(repos, tagChan)

	for entryErr := range tagChan {
		entry, err := entryErr.Entry, entryErr.Err
		if err != nil {
			return err
		}
		if e := bar.Add(1); e != nil {
			return e
		}
		entries = append(entries, entry)
	}

	return entries.Save(out)
}
