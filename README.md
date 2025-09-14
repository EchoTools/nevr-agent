# evr-data-recorder
session and player bone data recorder for EchoVR game engine.

A small command-line tool that records session and player 
bone data from the HTTP API of the EchoVR game engine.

Prerequisites (to build)
-	Go 1.25

Build
-	Build for the current OS:

```bash
go build -o datarecorder ./
```

-	Cross-compile for Linux (amd64):

```bash
GOOS=linux GOARCH=amd64 go build -o datarecorder ./
```

-	Cross-compile for Windows (amd64):

```bash
GOOS=windows GOARCH=amd64 go build -o datarecorder.exe ./
```

Run
-	Run the built binary (example):

The following will regularly scan ports 6721 through 6730, and start polling 
at upto 30 times a second, the HTTP API, storing to output to 

```bash
./datarecorder.exe -debug -frequency 30 -log agent.log -output ./output 127.0.0.1:6721-6730
```

Tests
-	Run unit tests:

```bash
go test ./...
```

