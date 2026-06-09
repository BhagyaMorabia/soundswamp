# Install script for directory: C:/Users/prsco/Desktop/bhagya/audio/client/android/app/.cxx/Debug/3p3fu3l5/armeabi-v7a/_deps/opus-src

# Set the install prefix
if(NOT DEFINED CMAKE_INSTALL_PREFIX)
  set(CMAKE_INSTALL_PREFIX "C:/Program Files (x86)/soundswarm_jni")
endif()
string(REGEX REPLACE "/$" "" CMAKE_INSTALL_PREFIX "${CMAKE_INSTALL_PREFIX}")

# Set the install configuration name.
if(NOT DEFINED CMAKE_INSTALL_CONFIG_NAME)
  if(BUILD_TYPE)
    string(REGEX REPLACE "^[^A-Za-z0-9_]+" ""
           CMAKE_INSTALL_CONFIG_NAME "${BUILD_TYPE}")
  else()
    set(CMAKE_INSTALL_CONFIG_NAME "Debug")
  endif()
  message(STATUS "Install configuration: \"${CMAKE_INSTALL_CONFIG_NAME}\"")
endif()

# Set the component getting installed.
if(NOT CMAKE_INSTALL_COMPONENT)
  if(COMPONENT)
    message(STATUS "Install component: \"${COMPONENT}\"")
    set(CMAKE_INSTALL_COMPONENT "${COMPONENT}")
  else()
    set(CMAKE_INSTALL_COMPONENT)
  endif()
endif()

# Install shared libraries without execute permission?
if(NOT DEFINED CMAKE_INSTALL_SO_NO_EXE)
  set(CMAKE_INSTALL_SO_NO_EXE "0")
endif()

# Is this installation the result of a crosscompile?
if(NOT DEFINED CMAKE_CROSSCOMPILING)
  set(CMAKE_CROSSCOMPILING "TRUE")
endif()

# Set default install directory permissions.
if(NOT DEFINED CMAKE_OBJDUMP)
  set(CMAKE_OBJDUMP "C:/Users/prsco/AppData/Local/Android/Sdk/ndk/25.1.8937393/toolchains/llvm/prebuilt/windows-x86_64/bin/llvm-objdump.exe")
endif()

if("x${CMAKE_INSTALL_COMPONENT}x" STREQUAL "xUnspecifiedx" OR NOT CMAKE_INSTALL_COMPONENT)
  file(INSTALL DESTINATION "${CMAKE_INSTALL_PREFIX}/lib" TYPE STATIC_LIBRARY FILES "C:/Users/prsco/Desktop/bhagya/audio/client/android/app/.cxx/Debug/3p3fu3l5/armeabi-v7a/_deps/opus-build/libopus.a")
endif()

if("x${CMAKE_INSTALL_COMPONENT}x" STREQUAL "xUnspecifiedx" OR NOT CMAKE_INSTALL_COMPONENT)
  file(INSTALL DESTINATION "${CMAKE_INSTALL_PREFIX}/include/opus" TYPE FILE FILES
    "C:/Users/prsco/Desktop/bhagya/audio/client/android/app/.cxx/Debug/3p3fu3l5/armeabi-v7a/_deps/opus-src/include/opus.h"
    "C:/Users/prsco/Desktop/bhagya/audio/client/android/app/.cxx/Debug/3p3fu3l5/armeabi-v7a/_deps/opus-src/include/opus_defines.h"
    "C:/Users/prsco/Desktop/bhagya/audio/client/android/app/.cxx/Debug/3p3fu3l5/armeabi-v7a/_deps/opus-src/include/opus_multistream.h"
    "C:/Users/prsco/Desktop/bhagya/audio/client/android/app/.cxx/Debug/3p3fu3l5/armeabi-v7a/_deps/opus-src/include/opus_projection.h"
    "C:/Users/prsco/Desktop/bhagya/audio/client/android/app/.cxx/Debug/3p3fu3l5/armeabi-v7a/_deps/opus-src/include/opus_types.h"
    )
endif()

if("x${CMAKE_INSTALL_COMPONENT}x" STREQUAL "xUnspecifiedx" OR NOT CMAKE_INSTALL_COMPONENT)
  file(INSTALL DESTINATION "${CMAKE_INSTALL_PREFIX}/lib/pkgconfig" TYPE FILE FILES "C:/Users/prsco/Desktop/bhagya/audio/client/android/app/.cxx/Debug/3p3fu3l5/armeabi-v7a/_deps/opus-build/opus.pc")
endif()

if("x${CMAKE_INSTALL_COMPONENT}x" STREQUAL "xUnspecifiedx" OR NOT CMAKE_INSTALL_COMPONENT)
  if(EXISTS "$ENV{DESTDIR}${CMAKE_INSTALL_PREFIX}/lib/cmake/Opus/OpusTargets.cmake")
    file(DIFFERENT EXPORT_FILE_CHANGED FILES
         "$ENV{DESTDIR}${CMAKE_INSTALL_PREFIX}/lib/cmake/Opus/OpusTargets.cmake"
         "C:/Users/prsco/Desktop/bhagya/audio/client/android/app/.cxx/Debug/3p3fu3l5/armeabi-v7a/_deps/opus-build/CMakeFiles/Export/lib/cmake/Opus/OpusTargets.cmake")
    if(EXPORT_FILE_CHANGED)
      file(GLOB OLD_CONFIG_FILES "$ENV{DESTDIR}${CMAKE_INSTALL_PREFIX}/lib/cmake/Opus/OpusTargets-*.cmake")
      if(OLD_CONFIG_FILES)
        message(STATUS "Old export file \"$ENV{DESTDIR}${CMAKE_INSTALL_PREFIX}/lib/cmake/Opus/OpusTargets.cmake\" will be replaced.  Removing files [${OLD_CONFIG_FILES}].")
        file(REMOVE ${OLD_CONFIG_FILES})
      endif()
    endif()
  endif()
  file(INSTALL DESTINATION "${CMAKE_INSTALL_PREFIX}/lib/cmake/Opus" TYPE FILE FILES "C:/Users/prsco/Desktop/bhagya/audio/client/android/app/.cxx/Debug/3p3fu3l5/armeabi-v7a/_deps/opus-build/CMakeFiles/Export/lib/cmake/Opus/OpusTargets.cmake")
  if("${CMAKE_INSTALL_CONFIG_NAME}" MATCHES "^([Dd][Ee][Bb][Uu][Gg])$")
    file(INSTALL DESTINATION "${CMAKE_INSTALL_PREFIX}/lib/cmake/Opus" TYPE FILE FILES "C:/Users/prsco/Desktop/bhagya/audio/client/android/app/.cxx/Debug/3p3fu3l5/armeabi-v7a/_deps/opus-build/CMakeFiles/Export/lib/cmake/Opus/OpusTargets-debug.cmake")
  endif()
endif()

if("x${CMAKE_INSTALL_COMPONENT}x" STREQUAL "xUnspecifiedx" OR NOT CMAKE_INSTALL_COMPONENT)
  file(INSTALL DESTINATION "${CMAKE_INSTALL_PREFIX}/lib/cmake/Opus" TYPE FILE FILES
    "C:/Users/prsco/Desktop/bhagya/audio/client/android/app/.cxx/Debug/3p3fu3l5/armeabi-v7a/_deps/opus-build/OpusConfig.cmake"
    "C:/Users/prsco/Desktop/bhagya/audio/client/android/app/.cxx/Debug/3p3fu3l5/armeabi-v7a/_deps/opus-build/OpusConfigVersion.cmake"
    )
endif()

