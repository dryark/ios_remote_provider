-include config.mk

TARGET = main

all: $(TARGET) bin/iosif repos/vidapp

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
	git clone $(config_repos_wda) repos/WebDriverAgent

repos/ujsonin:
	git clone https://github.com/nanoscopic/ujsonin.git repos/jsonin

bin/iosif: repos/iosif
	make -C repos/iosif

repos/iosif:
	git clone $(config_repos_iosif) repos/iosif

repos/vidapp:
	git clone $(config_repos_vidapp) repos/vidapp

vidtest_unsigned.xcarchive:
	xcodebuild -project repos/vidapp/vidtest/vidtest.xcodeproj -scheme vidtest archive -archivePath ./vidtest.xcarchive CODE_SIGN_IDENTITY="" CODE_SIGNING_REQUIRED=NO CODE_SIGNING_ALLOWED=NO

vidtest.xcarchive: repos/vidapp
	@./bin/gojq overlay -file1 default.json -file2 config.json -json > muxed.json
	./python/configure_vidtest.py muxed.json
	xcodebuild -project repos/vidapp/vidtest/vidtest.xcodeproj -scheme vidtest archive -archivePath ./vidtest.xcarchive

vidtest.ipa: vidtest.xcarchive repos/vidapp
	plutil -replace teamID -string $(config_vidtest_devTeamOu) ./repos/vidapp/vidtest/ExportOptions.plist
	@if [ -e vidtest.ipa ]; then rm vidtest.ipa; fi
	xcodebuild -exportArchive -archivePath ./vidtest.xcarchive -exportOptionsPlist ./repos/vidapp/vidtest/ExportOptions.plist -exportPath vidtest.ipa

vidtest.ipa/vidtest.ipa_x: vidtest.ipa
	mkdir vidtest.ipa/vidtest.ipa_x
	unzip vidtest.ipa/vidtest.ipa -d vidtest.ipa/vidtest.ipa_x
	find vidtest.ipa/vidtest.ipa_x | grep provision | xargs rm
	find vidtest.ipa/vidtest.ipa_x | grep _CodeSignature$$ | xargs rm -rf

vidtest_clean.ipa: vidtest.ipa/vidtest.ipa_x
	cd vidtest.ipa/vidtest.ipa_x && zip -r ../../vidtest_clean.ipa Payload

installvidapp: vidtest.xcarchive
	ios-deploy -b vidtest.xcarchive/Products/Applications/vidtest.app

vidtest_unsigned.ipa:
	@if [ -e tmp ]; then rm -rf tmp; fi
	mkdir tmp
	mkdir tmp/Payload
	ln -s ../../vidtest.xcarchive/Products/Applications/vidtest.app tmp/Payload/vidtest.app
	cd tmp && zip -r ../vidtest.ipa Payload

bin/gojq: repos/ujsonin
	make -C repos/ujsonin gojq

config.mk: config.json bin/gojq
	@./bin/gojq makevars -prefix config -file config.json -defaults default.json > config.mk

clonewda: repos/WebDriverAgent

wda: repos/WebDriverAgent/build

python/deps_installed: repos/mod-pbxproj
	pip3 install -r ./python/requires.txt
	touch python/deps_installed

repos/WebDriverAgent/build: repos/WebDriverAgent repos/mod-pbxproj config.json python/deps_installed bin/gojq
	@if [ -e repos/WebDriverAgent/build ]; then rm -rf repos/WebDriverAgent/build; fi;
	mkdir repos/WebDriverAgent/build
	@./bin/gojq overlay -file1 default.json -file2 config.json -json > muxed.json
	./python/configure_wda.py muxed.json
	cd repos/WebDriverAgent && xcodebuild -scheme WebDriverAgentRunner -allowProvisioningUpdates -destination generic/platform=iOS -derivedDataPath "./build" build-for-testing

repos/mod-pbxproj:
	git clone $(config_repos_pbxproj) repos/mod-pbxproj