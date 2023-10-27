
LDFLAGS=--ldflags '-extldflags "-static"'

GOTAGS= -tags containers_image_openpgp

ociv: *.go
	CGO_ENABLED=0 go build ${LDFLAGS} ${GOTAGS} -o ociv  ./...

test-image:
	stacker build -f example-stacker.yaml

.PHONY: clean
clean:
	stacker clean
	rm -rf ociv
