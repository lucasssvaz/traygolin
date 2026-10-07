PREFIX ?= /usr
BINDIR ?= $(PREFIX)/bin
LIBDIR ?= $(PREFIX)/lib
DATADIR ?= $(PREFIX)/share
LICENSEDIR ?= $(DATADIR)/licenses/traygolin
# polkit only reads actions from this directory, whatever PREFIX is.
POLKITDIR ?= /usr/share/polkit-1/actions
APPID := io.github.lucasssvaz.Traygolin
GO ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.1.0)
HELPER := $(LIBDIR)/traygolin/traygolin-helper
GO_LDFLAGS ?=
LDFLAGS_GO := $(GO_LDFLAGS) \
	-X github.com/lucasssvaz/traygolin/internal/metadata.Version=$(VERSION) \
	-X github.com/lucasssvaz/traygolin/internal/privhelper.HelperPath=$(HELPER)

.PHONY: all build test install uninstall schemas clean

all: build

build:
	$(GO) build -ldflags "$(LDFLAGS_GO)" -o bin/traygolin ./cmd/traygolin
	$(GO) build -ldflags "$(LDFLAGS_GO)" -o bin/traygolin-helper ./cmd/traygolin-helper

test:
	$(GO) test ./...

schemas:
	glib-compile-schemas .

bin/$(APPID).policy: $(APPID).policy.in
	@mkdir -p bin
	sed 's|@HELPER@|$(HELPER)|g' $< > $@

install: build bin/$(APPID).policy
	install -Dm755 bin/traygolin $(DESTDIR)$(BINDIR)/traygolin
	install -Dm755 bin/traygolin-helper $(DESTDIR)$(HELPER)
	install -Dm644 bin/$(APPID).policy $(DESTDIR)$(POLKITDIR)/$(APPID).policy
	install -Dm644 $(APPID).desktop $(DESTDIR)$(DATADIR)/applications/$(APPID).desktop
	install -Dm644 $(APPID).metainfo.xml $(DESTDIR)$(DATADIR)/metainfo/$(APPID).metainfo.xml
	install -Dm644 $(APPID).gschema.xml $(DESTDIR)$(DATADIR)/glib-2.0/schemas/$(APPID).gschema.xml
	install -Dm644 $(APPID).png $(DESTDIR)$(DATADIR)/icons/hicolor/256x256/apps/$(APPID).png
	install -Dm644 $(APPID).svg $(DESTDIR)$(DATADIR)/icons/hicolor/scalable/apps/$(APPID).svg
	install -Dm644 docs/traygolin.1 $(DESTDIR)$(DATADIR)/man/man1/traygolin.1
	install -Dm644 LICENSES/MIT-Trayscale.txt $(DESTDIR)$(LICENSEDIR)/MIT-Trayscale.txt
	install -Dm644 NOTICE $(DESTDIR)$(LICENSEDIR)/NOTICE
	# Packages must not ship gschemas.compiled; their schema hook builds it.
	[ -n "$(DESTDIR)" ] || glib-compile-schemas $(DATADIR)/glib-2.0/schemas || true

uninstall:
	rm -f $(DESTDIR)$(BINDIR)/traygolin
	rm -f $(DESTDIR)$(HELPER)
	rmdir $(DESTDIR)$(LIBDIR)/traygolin 2>/dev/null || true
	rm -f $(DESTDIR)$(POLKITDIR)/$(APPID).policy
	rm -f $(DESTDIR)$(DATADIR)/applications/$(APPID).desktop
	rm -f $(DESTDIR)$(DATADIR)/metainfo/$(APPID).metainfo.xml
	rm -f $(DESTDIR)$(DATADIR)/glib-2.0/schemas/$(APPID).gschema.xml
	rm -f $(DESTDIR)$(DATADIR)/icons/hicolor/256x256/apps/$(APPID).png
	rm -f $(DESTDIR)$(DATADIR)/icons/hicolor/scalable/apps/$(APPID).svg
	rm -f $(DESTDIR)$(DATADIR)/man/man1/traygolin.1
	rm -rf $(DESTDIR)$(LICENSEDIR)
	[ -n "$(DESTDIR)" ] || glib-compile-schemas $(DATADIR)/glib-2.0/schemas 2>/dev/null || true

clean:
	rm -rf bin gschemas.compiled
