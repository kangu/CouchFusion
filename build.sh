GOOS=linux GOARCH=amd64 go build -o ./build/linux-x86/couchfusion
echo "Linux amd64 build complete"

GOOS=linux GOARCH=arm64 go build -o ./build/linux-arm/couchfusion
echo "Linux arm64 build complete"

# macOS
GOOS=darwin GOARCH=amd64 go build -o ./build/darwin-amd64/couchfusion
echo "macOS amd64 build complete"

GOOS=darwin GOARCH=arm64 go build -o ./build/darwin-arm64/couchfusion
echo "macOS arm64 build complete"

# Windows
GOOS=windows GOARCH=amd64 go build -o ./build/windows/couchfusion.exe
echo "Windows build complete"
