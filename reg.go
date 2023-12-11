package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"

	ispec "github.com/opencontainers/image-spec/specs-go/v1"
	"golang.org/x/sync/semaphore"
)

type ImageNameSet map[string]interface{}

func (i ImageNameSet) HasName(name string) bool {
	_, ok := i[name]
	return ok
}

type LayerNameHashEntries []*LayerNameMapEntry

func (l LayerNameHashEntries) Save(file string) error {
	if len(l) == 0 {
		return fmt.Errorf("no entries to save")
	}

	if file == "" {
		return fmt.Errorf("filename is empty")
	}

	jsonBytes, err := json.Marshal(l)
	if err != nil {
		return err
	}

	return os.WriteFile(file, jsonBytes, 0644)
}

func Load(file string) (LayerNameHashEntries, error) {
	if file == "" {
		return nil, fmt.Errorf("filename is empty")
	}

	d, e := os.ReadFile(file)
	if e != nil {
		return nil, e
	}

	l := LayerNameHashEntries{}
	e = json.Unmarshal(d, &l)
	return l, e
}

func (l LayerNameHashEntries) ImageNames() ImageNameSet {
	ims := ImageNameSet{}
	for _, entry := range l {
		ims[entry.Name] = nil
	}
	return ims
}

type Reg struct {
	URL      string
	Prefixes []string
}

type RepoList struct {
	Repositories []string `json:"repositories"`
}

type Repo struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

func NewReg(url string, prefixes string) *Reg {
	return &Reg{
		URL:      url,
		Prefixes: strings.Split(prefixes, ","),
	}
}

func (r *Reg) GetRepoList(ctx context.Context) (*RepoList, error) {
	resp, err := http.Get(fmt.Sprintf("%s/v2/_catalog", r.URL))
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status code: %d", resp.StatusCode)
	}

	d, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	allRepos := &RepoList{}
	err = json.Unmarshal(d, allRepos)

	if len(r.Prefixes) == 0 {
		return allRepos, err
	}

	// Filter for repos starting with prefixes
	rl := &RepoList{Repositories: []string{}}
	for _, repo := range allRepos.Repositories {
		for _, prefix := range r.Prefixes {
			if strings.HasPrefix(repo, prefix) {
				rl.Repositories = append(rl.Repositories, repo)
			}
		}
	}

	return rl, err
}

func (r *Reg) GetRepoInfo(ctx context.Context, repo string) (*Repo, error) {
	resp, err := http.Get(fmt.Sprintf("%s/v2/%s/tags/list", r.URL, repo))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status code: %d", resp.StatusCode)
	}

	d, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	repoInfo := &Repo{}
	err = json.Unmarshal(d, repoInfo)
	return repoInfo, err
}

func (r *Reg) GetLayerNameEntry(ctx context.Context, repo, tag string) (*LayerNameMapEntry, error) {
	resp, err := http.Get(fmt.Sprintf("%s/v2/%s/manifests/%s", r.URL, repo, tag))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status code: %d", resp.StatusCode)
	}

	d, e := io.ReadAll(resp.Body)
	if e != nil {
		return nil, e
	}

	m := &ispec.Manifest{}
	if err := json.Unmarshal(d, m); err != nil {
		return nil, err
	}

	layer := m.Layers[len(m.Layers)-1]

	entry := &LayerNameMapEntry{
		Hash: string(layer.Digest)[7:], // trim off the "sha256:" prefix
		Name: fmt.Sprintf("%s:%s", repo, tag),
	}

	return entry, nil
}

type RepoError struct {
	Repo *Repo
	Err  error
}

type LayerEntryError struct {
	Entry *LayerNameMapEntry
	Err   error
}

func (r *Reg) FetchRepoInfos(l *RepoList, repos chan RepoError) {
	wg := sync.WaitGroup{}
	wg.Add(len(l.Repositories))
	sem := semaphore.NewWeighted(16)
	ctx := context.Background()
	for _, repo := range l.Repositories {
		// Create only 16 concurrent requests
		if err := sem.Acquire(ctx, 1); err != nil {
			panic(err)
		}

		go func(repo string) {
			defer wg.Done()
			defer sem.Release(1)
			r, e := r.GetRepoInfo(context.Background(), repo)
			repos <- RepoError{Repo: r, Err: e}
		}(repo)
	}

	wg.Wait()
	close(repos)
}

func (r *Reg) FetchLayerEntries(repos []*Repo, entries chan LayerEntryError) {
	wg := sync.WaitGroup{}
	sem := semaphore.NewWeighted(16)
	ctx := context.Background()
	for _, repo := range repos {
		for _, tag := range repo.Tags {
			wg.Add(1)
			if err := sem.Acquire(ctx, 1); err != nil {
				panic(err)
			}
			go func(name, tag string) {
				defer wg.Done()
				defer sem.Release(1)
				entry, err := r.GetLayerNameEntry(context.Background(), name, tag)
				entries <- LayerEntryError{Entry: entry, Err: err}
			}(repo.Name, tag)
		}
	}

	wg.Wait()
	close(entries)
}
