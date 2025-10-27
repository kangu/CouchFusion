GOOS=linux GOARCH=amd64 go build -o ./build/linux/cli-init
echo "Linux build complete"

# OSX
go build -o ./build/osx/cli-init
echo "OSX build complete"

# Windows
GOOS=windows GOARCH=amd64 go build -o ./build/windows/cli-init.exe
echo "Windows build complete"