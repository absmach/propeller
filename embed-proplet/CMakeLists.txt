cmake_minimum_required(VERSION 3.20.0)

find_package(Zephyr REQUIRED HINTS $ENV{ZEPHYR_BASE})
project(my_proplet)

add_subdirectory(src)

add_subdirectory(modules/wamr)

link_wamr_to_zephyr(app)

