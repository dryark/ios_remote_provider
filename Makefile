TARGET = main

all: $(TARGET)

$(TARGET): main.go proc_device_trigger.go proc_generic.go proc_backoff.go http_server.go wda.go controlfloor.go proc_ios_video_stream.go
	go build -o $(TARGET) .

go.sum:
	go get
	go get .

clean:
	$(RM) $(TARGET) go.sum
