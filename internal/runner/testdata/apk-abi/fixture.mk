TOPDIR := $(CURDIR)
STAGING_DIR_HOST := $(TOPDIR)
TARGET_DIR := $(TOPDIR)/root
PACKAGE_DIR := $(TOPDIR)/packages
DL_DIR := $(TOPDIR)/dl
include include/rootfs.mk
include abi-functions.mk

.PHONY: manifest image install installed
manifest image:
	@printf '%s\n' '$(call GetABISuffix,positive)' '$(call GetABISuffix,negative)'
install:
	@$(APK) add --arch fixture-arch --no-scripts fixture-package
installed:
	@$(APK) query --format json --fields name,version,tags --installed '*'
