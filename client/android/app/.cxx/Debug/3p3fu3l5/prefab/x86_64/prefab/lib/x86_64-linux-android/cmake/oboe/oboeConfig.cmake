if(NOT TARGET oboe::oboe)
add_library(oboe::oboe SHARED IMPORTED)
set_target_properties(oboe::oboe PROPERTIES
    IMPORTED_LOCATION "C:/Users/prsco/.gradle/caches/transforms-3/b0fcb5936d3f7a3798ad9bc6017cb689/transformed/jetified-oboe-1.8.0/prefab/modules/oboe/libs/android.x86_64/liboboe.so"
    INTERFACE_INCLUDE_DIRECTORIES "C:/Users/prsco/.gradle/caches/transforms-3/b0fcb5936d3f7a3798ad9bc6017cb689/transformed/jetified-oboe-1.8.0/prefab/modules/oboe/include"
    INTERFACE_LINK_LIBRARIES ""
)
endif()

