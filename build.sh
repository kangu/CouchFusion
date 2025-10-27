GOOS=linux GOARCH=amd64 go build -o ./build/linux-x86/cli-init
echo "Linux amd64 build complete"

GOOS=linux GOARCH=arm64 go build -o ./build/linux-arm/cli-init
echo "Linux arm64 build complete"

# OSX
go build -o ./build/osx/cli-init
echo "OSX build complete"

# Windows
GOOS=windows GOARCH=amd64 go build -o ./build/windows/cli-init.exe
echo "Windows build complete"