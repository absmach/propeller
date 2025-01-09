# my_proplet/CMakeLists.txt

cmake_minimum_required(VERSION 3.20.0)

# Standard Zephyr boilerplate
find_package(Zephyr REQUIRED HINTS $ENV{ZEPHYR_BASE})
project(my_proplet)

# Include our application source folder
add_subdirectory(src)

# Include WAMR as a subdirectory (the submodule)
add_subdirectory(modules/wamr)

# Link WAMR libraries to 'app' target
link_wamr_to_zephyr(app)

