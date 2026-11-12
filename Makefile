BINARY := prism

# Only what is made is put here: the binary by build, and the AppImage by
# appimage. Go compiles in a folder of its own in the temporary folder, and
# the AppImage is put together in BUILD_DIR.
BUILD_DIR := $(or $(TMPDIR),/tmp)/prism-build

# -s -w leaves out the symbol table and debug information, which roughly
# halves the size of the binary.
LDFLAGS := -s -w

.PHONY: build run test bench vet screenshot appimage clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

run: build
	./$(BINARY)

# Every run of the tests has a timeout, so that one that hangs fails with
# the stacks of what it was waiting on, rather than waiting for ever.
TEST_TIMEOUT := 5m

test:
	go test -timeout $(TEST_TIMEOUT) ./...

# Times what the window does over and over as it runs: a frame of the
# graphs, a reading, a refresh of the processes and the whole window drawn.
bench:
	go test -run '^$$' -bench . -benchmem -count 5 -timeout $(TEST_TIMEOUT) ./internal/ui/

vet:
	go vet ./...

# Draws the window, with a few minutes of made up readings, into
# docs/screenshot.png.
screenshot:
	mkdir -p docs
	PRISM_SCREENSHOT=$(CURDIR)/docs/screenshot.png go test -count=1 -timeout $(TEST_TIMEOUT) -run '^TestScreenshot$$' ./internal/ui/

# Packs the program and the Qt libraries it uses into one file that runs
# without installing anything. Releases are made by the GitHub workflow, on
# an old distribution so that they run on more systems.
appimage:
	BUILD_DIR=$(BUILD_DIR) packaging/appimage.sh

clean:
	rm -f $(BINARY) Prism-*.AppImage
	rm -rf $(BUILD_DIR)
