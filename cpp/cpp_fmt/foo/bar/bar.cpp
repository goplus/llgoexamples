#include <stdio.h>
#include <fmt/core.h>


extern "C" void Cprint(char* ss,int b) {   
        fmt::print(ss,b);
}