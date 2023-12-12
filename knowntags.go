package main

import (
	"fmt"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

// a hash->name pair to read in from the known layers json
type LayerNameMapEntry struct {
	Hash string `json:"hash"`
	Name string `json:"name"`
}

// global map of layer hashes to known names
var LayerNameMap = map[string][]string{}

// todo: be nice if this did a smart shortening, like replacing names with common prefixes and tags like
// foo.com/{imageone,imagetwo}:commontag

func getShortStringForNames(names []string) []string {
	// map from tag-> names
	namesWithCommonTags := map[string][]string{}
	truncatedLayerNames := []string{}

	if len(names) == 1 {
		return names
	}
	for _, name := range names {
		baseName := filepath.Base(name)
		ss := strings.Split(baseName, ":")
		if len(ss) == 2 {
			namesWithCommonTags[ss[1]] = append(namesWithCommonTags[ss[1]], ss[0])
		} else {
			namesWithCommonTags[""] = append(namesWithCommonTags[""], baseName)
		}
	}

	for tag, names := range namesWithCommonTags {
		tagstr := fmt.Sprintf("{%s}%s", strings.Join(names, ","), tag)
		truncatedLayerNames = append(truncatedLayerNames, tagstr)
	}

	return truncatedLayerNames
}

func getNamesForHash(digest string) []string {
	layerNames, ok := LayerNameMap[digest]
	if !ok {
		layerNames = []string{"?"}
	}
	return layerNames
}

func getUserOrSudoUserHomedir() string {
	username := os.Getenv("SUDO_USER")
	if username == "" {
		username = os.Getenv("USER")
	}
	u, err := user.Lookup(username)
	if err != nil {
		panic(err)
	}
	return u.HomeDir
}

func getKnowLayersFilename() string {
	userHome := getUserOrSudoUserHomedir()
	return filepath.Join(userHome, ".cache/ociv/known-layers.json")
}

func makeCacheDir() {
	userHome := getUserOrSudoUserHomedir()
	cacheDir := filepath.Join(userHome, ".cache/ociv")
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		if err := os.MkdirAll(cacheDir, 0755); err != nil {
			panic(err)
		}
	}
}

func setupWellKnownLayerNames() {
	fname := getKnowLayersFilename()
	entries, err := Load(fname)

	if err != nil {
		log.Printf("WARN: can't read %s: %s", fname, err.Error())

		return
	}

	for _, e := range entries {
		LayerNameMap[e.Hash] = append(LayerNameMap[e.Hash], e.Name)
	}
}
