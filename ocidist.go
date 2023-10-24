package main

import (
	"context"
	"encoding/json"
	"fmt"

	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/opencontainers/umoci/oci/casext"

	ispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// some code borrowed from raharper/ocidist:

func getBlob(layer *ispec.Descriptor, layoutpath string) ([]byte, error) {
	algo, digest, ok := strings.Cut(layer.Digest.String(), ":")
	if !ok {
		return []byte{}, fmt.Errorf("Failed to split layer digest '%s' into algo and hash", layer.Digest.Encoded())
	}

	blobPath := filepath.Join(layoutpath, "blobs", algo, digest)

	blobBytes, err := ioutil.ReadFile(blobPath)
	if err != nil {
		return []byte{}, fmt.Errorf("Failed to read OCI layer blob @ %q: %s", blobPath, err)
	}

	return blobBytes, nil
}

func getReferrersForImage(oci casext.Engine, layoutpath string, image *ispec.Descriptor) (*ispec.Index, error) {
	ociIndex, err := oci.GetIndex(context.Background())
	if err != nil {
		return nil, fmt.Errorf("Failed to get index from OCI Layout: %s", err)
	}

	refs := ispec.Index{
		MediaType: ispec.MediaTypeImageIndex,
	}
	for _, indexManifest := range ociIndex.Manifests {
		if indexManifest.MediaType == ispec.MediaTypeImageManifest && indexManifest.Digest != image.Digest {

			// get the blob @ manifest.Digest
			// we can't use oci since it doesn't yet support "subject" descriptors
			blob, err := getBlob(&indexManifest, layoutpath)
			if err != nil {
				return nil, fmt.Errorf("Failed to read index manifest blob: %s", err)
			}

			var refManifest ispec.Manifest
			if err := json.Unmarshal(blob, &refManifest); err != nil {
				return nil, fmt.Errorf("Failed to unmarshal index manifest blob into manifest: %s", err)
			}

			if refManifest.Subject == nil {
				continue
			}

			if refManifest.Subject.Digest == image.Digest {
				match := ispec.Descriptor{
					ArtifactType: refManifest.ArtifactType,
					MediaType:    indexManifest.MediaType,
					Digest:       indexManifest.Digest,
					Size:         indexManifest.Size,
				}
				refs.Manifests = append(refs.Manifests, match)
			}
		}
	}

	return &refs, nil
}
