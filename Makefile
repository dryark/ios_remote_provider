TARGET = main

all: $(TARGET)

provider_sources := $(wildcard *.go)

$(TARGET): $(provider_sources)
	go build -o $(TARGET) -tags macos .

go.sum:
	go get
	go get .

clean:
	$(RM) $(TARGET) go.sum
