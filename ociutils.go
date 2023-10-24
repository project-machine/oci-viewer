package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	ispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/opencontainers/umoci/oci/casext"

	"github.com/rivo/tview"
)

const OCIImageTitleAnnotation = "org.opencontainers.image.title"
const UmociUncompressedSizeAnnotation = "ci.umo.uncompressed_blob_size"

type imageref struct {
	layoutpath string
	tag        string
	hash       string

	// if image is a referrer
	targetTag  string
	targetHash string
}

func (ir *imageref) summary() string {
	info, ok := ImageInfoMap[ir.hash]
	if !ok {
		errmsg := fmt.Sprintf("no info for %+v", ir)
		log.Printf("%s\n", errmsg)
		return errmsg
	}
	return getImageInfoString(*ir, info)
}

func (ir imageref) searchString() []string {
	info, ok := ImageInfoMap[ir.hash]
	if !ok {
		errmsg := fmt.Sprintf("no info for %+v", ir)
		log.Printf("%s\n", errmsg)
		return []string{}
	}
	subjectHash, subjectName := info.getSubjectInfo()
	configStr := ""
	if len(info.config.History) > 0 {
		for _, histEntry := range info.config.History {
			configStr += fmt.Sprintf(" %s", histEntry.CreatedBy)
		}
	}
	annotationStr := ""
	for k, v := range info.manifest.Annotations {
		annotationStr += fmt.Sprintf("%s %s", k, v)
	}

	if info.configBlob.Descriptor.MediaType == "application/vnd.oci.image.config.v1+json" {
		config := info.configBlob.Data.(ispec.Image)
		configStr += fmt.Sprintf("%s %s", config.Config.Entrypoint, config.Config.Cmd)
	}
	searchComponents := []string{info.manifestDescriptor.Digest.String(),
		ir.layoutpath, ir.tag, ir.hash,
		info.manifest.ArtifactType,
		subjectHash, subjectName, configStr, annotationStr}
	searchComponents = append(searchComponents, info.layerDigests...)
	return searchComponents
}

type imageInfo struct {
	ref                imageref
	displayName        string // just the name
	displayLabel       string // a label, usually contains displayName but has other type info
	manifestDescriptor ispec.Descriptor
	manifest           ispec.Manifest
	configBlob         *casext.Blob
	config             ispec.Image
	layerDigests       []string
	filename           string // used for artifacts, when config is empty and there is one layer with a title annotation
	err                error
}

// return hash and name if available
func (ii *imageInfo) getSubjectInfo() (string, string) {
	if ii.manifest.Subject == nil {
		return "", ""
	}
	subjectName := "-"
	subjectHash := ii.manifest.Subject.Digest.String()[7:]
	subjInfo, ok := ImageInfoMap[subjectHash]
	if ok {
		subjectName = subjInfo.displayLabel
	}

	return subjectHash, subjectName
}

type subIndexRef struct {
	hash       string
	tag        string
	layoutpath string
}

type subIndexInfo struct {
	ref subIndexRef

	displayName         string
	displayLabel        string
	manifestDescriptors []ispec.Descriptor
	err                 error
}

func (sr subIndexRef) summary() string {
	info := SubIndexInfoMap[sr.hash]
	return fmt.Sprintf("sub-index with %d manifests", len(info.manifestDescriptors))
}

// ImageInfoMap - global map of manifest hashes to info
var ImageInfoMap = map[string]imageInfo{}

var SubIndexInfoMap = map[string]subIndexInfo{}

func displayStringForMediaType(mediatype string) string {
	switch mediatype {
	case ispec.MediaTypeImageLayer:
		return "Tar Image Layer"
	case ispec.MediaTypeImageLayerGzip:
		return "tgz Image Layer"
	case ispec.MediaTypeImageLayerZstd:
		return "zstd Image Layer"
	default:
		return mediatype
	}
}

func getImageInfoString(ref imageref, info imageInfo) string {

	log.Printf("getImageInfoString(ref %v)", ref)

	digest := info.manifestDescriptor.Digest.String()
	if len(digest) >= 7 {
		digest = digest[7:]
	} else {
		//log.Printf("HEY digest wasn't big enough for ref %+v? '%v'\n", ref, digest)
		digest = "error getting manifest descriptor!"
	}

	hdr := fmt.Sprintf("[yellow]# %s:%s\n[green]manifest blob path: [blue]%s[white]\n\n", filepath.Base(ref.layoutpath), info.displayName,
		filepath.Join(ref.layoutpath, "blobs", "sha256", digest))
	artifactType := "unset"
	if info.manifest.ArtifactType != "" {
		artifactType = info.manifest.ArtifactType
	}
	hdr += fmt.Sprintf("[yellow]# ArtifactType: [blue]%s[white]\n\n", artifactType)

	if info.err != nil {
		errmsg := fmt.Sprintf("\nERROR reading image: %v\n", info.err)
		hdr += fmt.Sprintf("\n[red:yellow]ERROR reading image: %v[white:-]\n", info.err)
		log.Printf(errmsg)
	}

	subjectHash, subjectName := info.getSubjectInfo()
	if subjectHash != "" {
		hdr += fmt.Sprintf("\n[yellow]# Referrer Info:\n[green]subject hash: [blue]%s\n[green]subject name: %s\n\n",
			subjectHash, subjectName)
	}

	manifestBuf := new(bytes.Buffer)
	manifestTW := tabwriter.NewWriter(manifestBuf, 1, 1, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(manifestTW, strings.Join([]string{"[blue]blob sha[white]", "tar sha", "names", "type", "created", "sz (kb)", "tar sz (kb)", "author"}, "\t")+"\t")
	manifestTableHeader := fmt.Sprintf("[yellow]# %d layers in manifest[white]\n(note tar* fields refer to the uncompressed blob)\n", len(info.manifest.Layers))

	for idx, layer := range info.manifest.Layers {
		digest := layer.Digest.String()[7:]

		uncompressedSizeAnnotation := "missing"
		if val, ok := layer.Annotations[UmociUncompressedSizeAnnotation]; ok {
			uncompressedSizeAnnotation = fmt.Sprintf("%d", val)
		}

		var layerNames = getShortNamesForHash(digest)

		diffIDHash := "-"
		if len(info.config.RootFS.DiffIDs) > idx {
			diffIDHash = info.config.RootFS.DiffIDs[idx].String()[7:14]
		}

		fmt.Fprintln(manifestTW, strings.Join([]string{
			fmt.Sprintf("[blue]%s[white]", digest[:7]),
			diffIDHash,
			strings.Join(layerNames, ","),
			displayStringForMediaType(layer.MediaType),
			"-",
			fmt.Sprintf("%d", layer.Size/1024.0),
			uncompressedSizeAnnotation,
			"-"}, "\t")+"\t")

	}
	manifestTW.Flush()

	// TODO make config history collapsible
	cfgHistBuf := new(bytes.Buffer)
	cfgHistHeader := ""
	if len(info.config.History) > 0 {
		cfgHistHeader = fmt.Sprintf("\n\n[yellow]# %d entries in Runtime Config History:[white]\n(note, some entries here do not correspond to blob layers)\n", len(info.config.History))
		cfgHistTW := tabwriter.NewWriter(cfgHistBuf, 1, 1, 2, ' ', tabwriter.AlignRight)
		fmt.Fprintln(cfgHistTW, strings.Join([]string{"[blue]blob digest[white]", "names", "type", "created", "blob size (kb)", "author"}, "\t")+"\t")

		layerIdx := 0
		for _, histEntry := range info.config.History {
			if histEntry.EmptyLayer {
				// only show fields from history entry, there is no matching layer from the manifest:
				fmt.Fprintln(cfgHistTW, strings.Join([]string{
					"[grey]empty[white]",
					"-",            // name
					"(cfg update)", // mediatype
					histEntry.Created.Format(time.RFC822),
					"-",
					histEntry.CreatedBy}, "\t")+"\t")
				continue
			}

			layer := info.manifest.Layers[layerIdx]
			digest := layer.Digest.String()[7:]

			var layerNames = getNamesForHash(digest)

			fmt.Fprintln(cfgHistTW, strings.Join([]string{
				fmt.Sprintf("[blue]%s[white]", digest[:7]),
				strings.Join(layerNames, ","),
				displayStringForMediaType(layer.MediaType),
				histEntry.Created.Format(time.RFC822),
				fmt.Sprintf("%d", layer.Size/1024.0),
				histEntry.CreatedBy}, "\t")+"\t")
			layerIdx++
		}
		cfgHistTW.Flush()
	}

	configInfo := "no config"
	if info.configBlob != nil {
		log.Printf("config blob desc mediatype is %s\n", info.configBlob.Descriptor.MediaType)

		switch info.configBlob.Descriptor.MediaType {
		case "application/vnd.oci.image.manifest.v1+json":
			configInfo = fmt.Sprintf("got a manifest configblob mediatype, expected?")

		case "application/vnd.dev.cosign.artifact.sig.v1+json":
			configInfo = "TODO: cosign artifact"
		case "application/vnd.oci.image.config.v1+json":
			config := info.configBlob.Data.(ispec.Image)

			configInfo = tview.Escape(fmt.Sprintf("Entrypoint: %s\nCmd: %s",
				config.Config.Entrypoint, config.Config.Cmd))
		case "application/vnd.cncf.notary.signature":
			configInfo = "Notary Signatures have empty Config"
		case "application/vnd.oci.empty.v1+json":
			configInfo = "No Config"
		default:
			configInfo = fmt.Sprintf("parsing %q not yet supported. \nblob data is %+v\n blob is %+v", info.configBlob.Descriptor.MediaType,
				info.configBlob.Data,
				info.configBlob,
			)
		}
	}
	annotationstr := ""
	for k, v := range info.manifest.Annotations {
		annotationstr += fmt.Sprintf("[green]%s[white]:\n%s\n\n", k, tview.Escape(v))
	}

	// TODO make config history collapsible
	// return hdr + manifestTableHeader + manifestBuf.String() + "\n\n[yellow]# Config[white]\n" + configInfo + "\n\n[yellow]# Annotations[white]\n" + annotationstr
	return hdr + manifestTableHeader + manifestBuf.String() + cfgHistHeader + cfgHistBuf.String() + "\n\n[yellow]# Config[white]\n" + configInfo + "\n\n[yellow]# Annotations[white]\n" + annotationstr
}

func isOCILayout(path string) bool {
	if _, err := os.Stat(filepath.Join(path, "index.json")); errors.Is(err, os.ErrNotExist) {
		return false
	}
	return true
}

func loadSubIndexManifest(oci casext.Engine, ref subIndexRef, manifestDescriptor ispec.Descriptor) subIndexInfo {
	info := subIndexInfo{
		ref: ref,
	}

	manifestBlob, err := oci.FromDescriptor(context.Background(), manifestDescriptor)
	if err != nil {
		log.Printf("error getting manifest blob for tag %q: '%v'", ref.tag, err)
		info.err = err
		return info
	}
	index, ok := manifestBlob.Data.(ispec.Index)
	if !ok {
		log.Printf("error casting manifest blob data as an index for tag %s", ref.tag)
		info.err = fmt.Errorf("couldn't read manifest blob")
		return info
	}

	info.manifestDescriptors = index.Manifests

	info.displayLabel = fmt.Sprintf("Subindex '%s' with %d manifests", ref.tag, len(index.Manifests))
	info.displayName = fmt.Sprintf("subindex '%s'", ref.tag)

	return info
}

func loadImageManifest(oci casext.Engine, ref imageref, manifestDescriptor ispec.Descriptor) imageInfo {
	info := imageInfo{
		ref:                ref,
		manifestDescriptor: manifestDescriptor,
	}
	log.Printf("\n\nloadImageManifest loading %+v, descriptor %+v", ref, manifestDescriptor)

	if manifestDescriptor.MediaType != ispec.MediaTypeImageManifest {
		log.Printf("\nerror - was expecting an image manifest, got: %+v", manifestDescriptor)
		info.err = fmt.Errorf("expecting image manifest, got %+v", manifestDescriptor)
		return info
	}

	manifestBlob, err := oci.FromDescriptor(context.Background(), manifestDescriptor)
	if err != nil {
		log.Printf("error getting manifest blob for tag %q: '%v'", ref.tag, err)
		info.err = err
		return info
	}
	manifest, ok := manifestBlob.Data.(ispec.Manifest)
	if !ok {
		log.Printf("error casting manifest blob data as a manifest for tag %s", ref.tag)
		info.err = fmt.Errorf("couldn't read manifest blob")
		return info
	}
	info.manifest = manifest

	configBlob, err := oci.FromDescriptor(context.Background(), manifest.Config)
	if err != nil {
		log.Printf("error getting config blob for tag %s: %v", ref.tag, err)
		info.err = err
		return info
	}
	log.Printf("configblob for tag %s is %+v", ref.tag, configBlob)
	info.configBlob = configBlob

	config, ok := configBlob.Data.(ispec.Image)
	if !ok {
		log.Printf("[internal error] unknown config blob type: %s", configBlob.Descriptor.MediaType)
	} else {
		info.config = config
	}

	// add this image's tag as a known name for the top layer's digest
	topLayer := info.manifest.Layers[len(info.manifest.Layers)-1]
	dgst := topLayer.Digest.String()[7:]
	LayerNameMap[dgst] = append(LayerNameMap[dgst], ref.tag)

	for _, layer := range info.manifest.Layers {
		dgst := layer.Digest.String()[7:]
		info.layerDigests = append(info.layerDigests, dgst)
	}

	// set the displayName based on the kind of thing this is
	// if it's a tagged image, just use that:
	if ref.tag != "" {
		info.displayLabel = fmt.Sprintf("🏷  image %q", ref.tag)
		info.displayName = ref.tag
	} else {
		switch configBlob.Descriptor.MediaType {
		case ispec.MediaTypeImageConfig:
			info.displayLabel = fmt.Sprintf("💾 image %q", ref.hash)
			info.displayName = dgst
		case "application/vnd.oci.empty.v1+json":
			filename := ""
			if len(info.manifest.Layers) == 1 {
				filename = filepath.Base(info.manifest.Layers[0].Annotations[OCIImageTitleAnnotation])
			}
			info.filename = filename
			info.displayLabel = fmt.Sprintf("🗄  %q (%s)", info.filename, info.manifest.ArtifactType)
			info.displayName = dgst
		case "application/vnd.cncf.notary.signature":
			info.displayLabel = fmt.Sprintf("🔒 Notary Signature %s", ref.hash)
			info.displayName = fmt.Sprintf("Notary Signature %s", ref.hash)
		case "application/vnd.oci.image.index.v1+json":
			info.displayLabel = fmt.Sprintf("🗂  Notary Signature Index")
			info.displayName = fmt.Sprintf("Notary Signature Index")
		default:
			info.displayLabel = fmt.Sprintf("unknown mediatype. %s %s", configBlob.Descriptor.MediaType, ref.hash)
			info.displayName = configBlob.Descriptor.MediaType
		}
	}

	return info
}
