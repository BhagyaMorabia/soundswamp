# This file will be configured to contain variables for CPack. These variables
# should be set in the CMake list file of the project before CPack module is
# included. The list of available CPACK_xxx variables and their associated
# documentation may be obtained using
#  cpack --help-variable-list
#
# Some variables are common to all generators (e.g. CPACK_PACKAGE_NAME)
# and some are specific to a generator
# (e.g. CPACK_NSIS_EXTRA_INSTALL_COMMANDS). The generator specific variables
# usually begin with CPACK_<GENNAME>_xxxx.


set(CPACK_BUILD_SOURCE_DIRS "C:/Users/prsco/Desktop/bhagya/audio/client/android/app/src/main/cpp;C:/Users/prsco/Desktop/bhagya/audio/client/android/app/.cxx/Debug/3p3fu3l5/arm64-v8a")
set(CPACK_CMAKE_GENERATOR "Ninja")
set(CPACK_COMPONENT_UNSPECIFIED_HIDDEN "TRUE")
set(CPACK_COMPONENT_UNSPECIFIED_REQUIRED "TRUE")
set(CPACK_DEFAULT_PACKAGE_DESCRIPTION_FILE "C:/Users/prsco/AppData/Local/Android/Sdk/cmake/3.22.1/share/cmake-3.22/Templates/CPack.GenericDescription.txt")
set(CPACK_DEFAULT_PACKAGE_DESCRIPTION_SUMMARY "soundswarm_jni built using CMake")
set(CPACK_GENERATOR "TGZ")
set(CPACK_INSTALL_CMAKE_PROJECTS "C:/Users/prsco/Desktop/bhagya/audio/client/android/app/.cxx/Debug/3p3fu3l5/arm64-v8a;soundswarm_jni;ALL;/")
set(CPACK_INSTALL_PREFIX "C:/Program Files (x86)/soundswarm_jni")
set(CPACK_MODULE_PATH "C:/Users/prsco/Desktop/bhagya/audio/client/android/app/.cxx/Debug/3p3fu3l5/arm64-v8a/_deps/opus-src/cmake")
set(CPACK_NSIS_DISPLAY_NAME "soundswarm_jni 0.1.1")
set(CPACK_NSIS_INSTALLER_ICON_CODE "")
set(CPACK_NSIS_INSTALLER_MUI_ICON_CODE "")
set(CPACK_NSIS_INSTALL_ROOT "$PROGRAMFILES")
set(CPACK_NSIS_PACKAGE_NAME "soundswarm_jni 0.1.1")
set(CPACK_NSIS_UNINSTALL_NAME "Uninstall")
set(CPACK_OUTPUT_CONFIG_FILE "C:/Users/prsco/Desktop/bhagya/audio/client/android/app/.cxx/Debug/3p3fu3l5/arm64-v8a/CPackConfig.cmake")
set(CPACK_PACKAGE_DEFAULT_LOCATION "/")
set(CPACK_PACKAGE_DESCRIPTION_FILE "C:/Users/prsco/AppData/Local/Android/Sdk/cmake/3.22.1/share/cmake-3.22/Templates/CPack.GenericDescription.txt")
set(CPACK_PACKAGE_DESCRIPTION_SUMMARY "soundswarm_jni built using CMake")
set(CPACK_PACKAGE_FILE_NAME "soundswarm_jni-0.1.1-Android")
set(CPACK_PACKAGE_INSTALL_DIRECTORY "soundswarm_jni 0.1.1")
set(CPACK_PACKAGE_INSTALL_REGISTRY_KEY "soundswarm_jni 0.1.1")
set(CPACK_PACKAGE_NAME "soundswarm_jni")
set(CPACK_PACKAGE_RELOCATABLE "true")
set(CPACK_PACKAGE_VENDOR "Humanity")
set(CPACK_PACKAGE_VERSION "0.1.1")
set(CPACK_PACKAGE_VERSION_MAJOR "0")
set(CPACK_PACKAGE_VERSION_MINOR "1")
set(CPACK_PACKAGE_VERSION_PATCH "1")
set(CPACK_RESOURCE_FILE_LICENSE "C:/Users/prsco/AppData/Local/Android/Sdk/cmake/3.22.1/share/cmake-3.22/Templates/CPack.GenericLicense.txt")
set(CPACK_RESOURCE_FILE_README "C:/Users/prsco/AppData/Local/Android/Sdk/cmake/3.22.1/share/cmake-3.22/Templates/CPack.GenericDescription.txt")
set(CPACK_RESOURCE_FILE_WELCOME "C:/Users/prsco/AppData/Local/Android/Sdk/cmake/3.22.1/share/cmake-3.22/Templates/CPack.GenericWelcome.txt")
set(CPACK_SET_DESTDIR "OFF")
set(CPACK_SOURCE_GENERATOR "TBZ2;TGZ;TXZ;TZ")
set(CPACK_SOURCE_OUTPUT_CONFIG_FILE "C:/Users/prsco/Desktop/bhagya/audio/client/android/app/.cxx/Debug/3p3fu3l5/arm64-v8a/CPackSourceConfig.cmake")
set(CPACK_SOURCE_RPM "OFF")
set(CPACK_SOURCE_TBZ2 "ON")
set(CPACK_SOURCE_TGZ "ON")
set(CPACK_SOURCE_TXZ "ON")
set(CPACK_SOURCE_TZ "ON")
set(CPACK_SOURCE_ZIP "OFF")
set(CPACK_SYSTEM_NAME "Android")
set(CPACK_THREADS "1")
set(CPACK_TOPLEVEL_TAG "Android")
set(CPACK_WIX_SIZEOF_VOID_P "8")

if(NOT CPACK_PROPERTIES_FILE)
  set(CPACK_PROPERTIES_FILE "C:/Users/prsco/Desktop/bhagya/audio/client/android/app/.cxx/Debug/3p3fu3l5/arm64-v8a/CPackProperties.cmake")
endif()

if(EXISTS ${CPACK_PROPERTIES_FILE})
  include(${CPACK_PROPERTIES_FILE})
endif()
