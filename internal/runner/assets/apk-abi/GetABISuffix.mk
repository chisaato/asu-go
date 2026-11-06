define GetABISuffix
$(shell $(if $(strip $(FAKEROOT)),$(subst $(FAKEROOT) ,python3 "$(TOPDIR)/.asu/patches/apk-abi/query.py" ,$(APK)),$(APK)) query --fields tags --match provides $(1) | grep openwrt:abiversion | awk -F= '{print $$2; exit}')
endef
