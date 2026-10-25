BINARY := prism

# -s -w leaves out the symbol table and debug information, which roughly
# halves the size of the binary.
LDFLAGS := -s -w

.PHONY: build run test vet screenshot icons appimage macos clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

run: build
	./$(BINARY)

# Every run of the tests has a timeout, so that one that hangs fails with
# the stacks of what it was waiting on, rather than waiting for ever.
TEST_TIMEOUT := 5m

test:
	go test -timeout $(TEST_TIMEOUT) ./...

vet:
	go vet ./...

# Draws the window, with a few minutes of made up readings, into
# docs/screenshot.png.
screenshot:
	mkdir -p docs
	PRISM_SCREENSHOT=$(CURDIR)/docs/screenshot.png go test -count=1 -timeout $(TEST_TIMEOUT) -run '^TestScreenshot$$' ./internal/ui/

# Draws the program's icon, internal/appicon/appicon.png, and that of the
# macOS app, packaging/macos/prism.png. Run it again whenever the
# drawing changes; a test says when it has not been.
icons:
	PRISM_ICONS=$(CURDIR) go test -count=1 -timeout $(TEST_TIMEOUT) -run '^TestWriteIcons$$' ./internal/appicon/

# Packs the program and the Qt libraries it uses into one file that runs
# without installing anything. Releases are made by the GitHub workflow, on
# an old distribution so that they run on more systems.
appimage:
	packaging/appimage.sh

# Builds the macOS app for Apple Silicon, with Qt inside it, in a disk image.
# It runs on a Mac with Homebrew's Qt; releases are made by the GitHub
# workflow.
macos:
	packaging/macos.sh

clean:
	rm -f $(BINARY)
	rm -rf build
