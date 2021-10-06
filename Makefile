-include config.mk

TARGET = main

all: $(TARGET) repos/vidapp/versionMarker bin/go-ios

bin/gojq: repos/ujsonin/versionMarker
	make -C repos/ujsonin gojq && touch bin/gojq

bin/go-ios: repos/go-ios/versionMarker
	cd repos/go-ios && go build .
	touch bin/go-ios

config.mk: config.json bin/gojq
	@rm -rf config.mk
	@./bin/gojq makevars -prefix config -file config.json -defaults default.json -outfile config.mk

provider_sources := $(wildcard *.go)

$(TARGET): config.mk $(provider_sources) go.mod
	@if [ "$(config_jsonfail)" == "1" ]; then\
		echo $(config_jsonerr) ;\
		exit 1;\
	fi
	go build -o $(TARGET) -tags macos .

go.sum:
	go get
	go get .

clean:
	$(RM) $(TARGET)

wdaclean:
	$(RM) -rf repos/WebDriverAgent/build

cfaclean:
	$(RM) -rf repos/CFAgent/build

repos/WebDriverAgent/versionMarker: repos/WebDriverAgent repos/versionMarkers/WebDriverAgent
	cd repos/WebDriverAgent && git pull
	touch repos/WebDriverAgent/versionMarker

repos/CFAgent/versionMarker: repos/CFAgent repos/versionMarkers/CFAgent
	cd repos/CFAgent && git pull
	touch repos/CFAgent/versionMarker

repos/WebDriverAgent:
	git clone $(config_repos_wda) repos/WebDriverAgent

repos/CFAgent:
	git clone $(config_repos_cfa) repos/CFAgent

repos/ujsonin/versionMarker: repos/ujsonin repos/versionMarkers/ujsonin
	cd repos/ujsonin && git pull
	touch repos/ujsonin/versionMarker

repos/ujsonin:
	git clone https://github.com/nanoscopic/ujsonin.git repos/ujsonin
	touch repos/ujsonin/versionMarker

bin/iosif: repos/iosif/versionMarker
	make -C repos/iosif
	touch bin/iosif

repos/iosif/versionMarker: repos/iosif repos/versionMarkers/iosif
	cd repos/iosif && git pull
	touch repos/iosif/versionMarker

repos/iosif:
	git clone $(config_repos_iosif) repos/iosif

repos/vidapp/versionMarker: repos/vidapp repos/versionMarkers/vidapp
	cd repos/vidapp && git pull
	touch repos/vidapp/versionMarker

repos/vidapp:
	git clone $(config_repos_vidapp) repos/vidapp

repos/go-ios/versionMarker: repos/go-ios repos/versionMarkers/go-ios
	cd repos/go-ios && git pull
	touch repos/go-ios/versionMarker

repos/go-ios:
	git clone $(config_repos_goios) repos/go-ios

vidstream_unsigned.xcarchive:
	xcodebuild -project repos/vidapp/vidstream/vidstream.xcodeproj -scheme vidstream archive -archivePath ./vidstream.xcarchive CODE_SIGN_IDENTITY="" CODE_SIGNING_REQUIRED=NO CODE_SIGNING_ALLOWED=NO

vidstream.xcarchive: repos/vidapp
	@./bin/gojq overlay -file1 default.json -file2 config.json -json > muxed.json
	./python/configure_vidstream.py muxed.json
	xcodebuild -project repos/vidapp/vidstream/vidstream.xcodeproj -scheme vidstream archive -archivePath ./vidstream.xcarchive

vidstream.ipa: vidstream.xcarchive repos/vidapp
	plutil -replace teamID -string $(config_vidstream_devTeamOu) ./repos/vidapp/vidstream/ExportOptions.plist
	@if [ -e vidstream.ipa ]; then rm vidstream.ipa; fi
	xcodebuild -exportArchive -archivePath ./vidstream.xcarchive -exportOptionsPlist ./repos/vidapp/vidstream/ExportOptions.plist -exportPath vidstream.ipa

vidstream.ipa/vidstream.ipa_x: vidstream.ipa
	mkdir vidstream.ipa/vidstream.ipa_x
	unzip vidstream.ipa/vidstream.ipa -d vidstream.ipa/vidstream.ipa_x
	find vidstream.ipa/vidstream.ipa_x | grep provision | xargs rm
	find vidstream.ipa/vidstream.ipa_x | grep _CodeSignature$$ | xargs rm -rf

vidstream_clean.ipa: vidstream.ipa/vidstream.ipa_x
	cd vidstream.ipa/vidstream.ipa_x && zip -r ../../vidstream_clean.ipa Payload

installvidapp: vidstream.xcarchive
	ios-deploy -b vidstream.xcarchive/Products/Applications/vidstream.app

vidstream_unsigned.ipa:
	@if [ -e tmp ]; then rm -rf tmp; fi
	mkdir tmp
	mkdir tmp/Payload
	ln -s ../../vidstream.xcarchive/Products/Applications/vidstream.app tmp/Payload/vidstream.app
	cd tmp && zip -r ../vidstream.ipa Payload

clonewda: repos/WebDriverAgent

clonecfa: repos/CFAgent

wda: repos/WebDriverAgent/build

cfa: repos/CFAgent/build

vidapp: repos/vidapp

python/deps_installed: repos/mod-pbxproj
	pip3 install -r ./python/requires.txt
	touch python/deps_installed

repos/WebDriverAgent/build: repos/WebDriverAgent/versionMarker repos/mod-pbxproj config.json python/deps_installed bin/gojq
	@if [ -e repos/WebDriverAgent/build ]; then rm -rf repos/WebDriverAgent/build; fi;
	mkdir repos/WebDriverAgent/build
	@./bin/gojq overlay -file1 default.json -file2 config.json -json > muxed.json
	./python/configure_wda.py muxed.json
	cd repos/WebDriverAgent && xcodebuild -scheme WebDriverAgentRunner -allowProvisioningUpdates -destination generic/platform=iOS -derivedDataPath "./build" build-for-testing

repos/CFAgent/build: repos/CFAgent/versionMarker repos/mod-pbxproj config.json python/deps_installed bin/gojq
	@if [ -e repos/CFAgent/build ]; then rm -rf repos/CFAgent/build; fi;
	mkdir repos/CFAgent/build
	@./bin/gojq overlay -file1 default.json -file2 config.json -json > muxed.json
	./python/configure_cfa.py muxed.json
	cd repos/CFAgent && xcodebuild -scheme CFAgent -allowProvisioningUpdates -destination generic/platform=iOS -derivedDataPath "./build" build-for-testing

repos/mod-pbxproj:
	git clone $(config_repos_pbxproj) repos/mod-pbxproj

usetidevice: calculated.json

calculated.json: bin/gojq repos/versionMarkers/calculated_json
	$(eval TIDEVICE_PKGS_PATH := $(shell pip3 show tidevice | grep Location | cut -c 11-))
	$(eval TIDEVICE_BIN_PATH := $(shell pip3 show -f tidevice | grep bin/tidevice))
	@./bin/gojq set -file calculated.json -path tidevice -val $(TIDEVICE_PKGS_PATH)/$(TIDEVICE_BIN_PATH)
	
