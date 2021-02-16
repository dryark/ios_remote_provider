-include config.mk

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

wdaclean:
	$(RM) -rf repos/WebDriverAgent/build

repos/WebDriverAgent:
	git clone "$(config_repos_wda)" repos/WebDriverAgent

repos/ujsonin:
	git clone "$(config_repos_ujsonin)" repos/jsonin

repos/iosif:
	git clone "$(config_repos_iosif)" repos/iosif

bin/gojq: repos/ujsonin
	make -c repos/ujsonin gojq

config.mk: config.json
	@./bin/gojq makevars -prefix config -file config.json -defaults default.json > config.mk

clonewda: repos/WebDriverAgent

wda: repos/WebDriverAgent/build

python/deps_installed: repos/mod-pbxproj
	pip3 install -r ./python/requires.txt
	touch python/deps_installed

repos/WebDriverAgent/build: repos/WebDriverAgent repos/mod-pbxproj config.json python/deps_installed
	@if [ -e repos/WebDriverAgent/build ]; then rm -rf repos/WebDriverAgent/build; fi;
	mkdir repos/WebDriverAgent/build
	@./bin/gojq overlay -file1 default.json -file2 config.json -json > muxed.json
	./python/configure_wda.py muxed.json
	cd repos/WebDriverAgent && xcodebuild -scheme WebDriverAgentRunner -allowProvisioningUpdates -destination generic/platform=iOS -derivedDataPath "./build" build-for-testing

repos/mod-pbxproj:
	git clone "$(config_repos_pbxproj)" repos/mod-pbxproj