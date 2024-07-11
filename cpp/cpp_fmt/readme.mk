# fmt
The fmt library is a modern C++ library for formatting text, which is safer and more efficient than traditional printf and iostream. It provides an interface similar to Python's str.format() and modern C++, making formatting strings more convenient and intuitive.

## Clone the install fmt

### Mac OS
```sh
brew install fmt
```

## generate dylib 
Readers should operate in the ```/cpp_fmt``` directory
```sh
cd /foo
g++ -std=c++11 -fPIC -shared bar.cpp -o libfmtutils.dylib -lfmt
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
llgo run cpp_fmt.go
```

