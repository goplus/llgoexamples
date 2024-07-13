# LLGo wrapper of csv

## How to install

### Clone & Build Repository

```
git clone https://github.com/goplus/llgoexamples
cd lib/rust/csv-wrapper
cargo build --release
```

### Install dylib-installer

Install the dylib-installer tool, which is used to install dynamic libraries:

```bash
brew tap hackerchai/tap
brew install dylib-installer
```

Or you can install it using Cargo:

```bash 
cargo install dylib_installer
```

### Install Dynamic Library

Use dylib-installer to install the built dynamic library into the system directory:

```
sudo dylib_installer -d ./target/release/
```

## Demos

- [csvdemo](_demo/csvdemo/csv.go): a basic csv demo

### How to run demos

To run the demos in directory `_demo`:

```sh
cd <demo-directory>  # eg. cd _demo/csvdemo
llgo run .
```
