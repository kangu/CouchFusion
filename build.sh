GOOS=linux GOARCH=amd64 go build -o ./build/linux-x86/couchfusion
echo "Linux amd64 build complete"

GOOS=linux GOARCH=arm64 go build -o ./build/linux-arm/couchfusion
echo "Linux arm64 build complete"

# OSX
go build -o ./build/osx/couchfusion
echo "OSX build complete"

# Windows
GOOS=windows GOARCH=amd64 go build -o ./build/windows/couchfusion.exe
echo "Windows build complete"
