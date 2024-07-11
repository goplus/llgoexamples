# SPDLOG
spdlog is a lightweight C++ logging library.

## Clone the install spdlog
```sh
git clone https://github.com/goplus/llgoexamples.git
brew install spdlog
```

## generate dylib 
```sh
cd cppWrap/lib
g++ -std=c++11 -fPIC -shared cppWrap.cpp -o libcppWrap.dylib -lspdlog -lfmt
```

## Install dylib-installer

```sh
brew tap hackerchai/tap
brew install dylib-installer
```

## Install Dynamic Library
```sh
sudo dylib_installer /path/to/dylibs
```

## run demo
```sh
cd _demo
llgo run Demo1.go
```

