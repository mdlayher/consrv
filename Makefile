.PHONY: all

all:
	GOARCH=arm64 CGO_ENABLED=0 go build \
	&& tar cf breakglass.tar --dereference consrv \
	&& breakglass -debug_tarball_pattern breakglass.tar monitnerr-1
