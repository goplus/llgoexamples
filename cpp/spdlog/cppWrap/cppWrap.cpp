#include "spdlog/spdlog.h"
#include "stdarg.h"

 extern "C" void PrintInfo(const char* msg){
    spdlog::info(msg);
}


 extern "C" void PrintError(const char* msg){
    spdlog::error(msg);
}

 extern "C" void PrintWarn(const char* msg){
    spdlog::warn(msg);
}

 extern "C" void PrintCritical(const char* msg){
    spdlog::critical(msg);
}

 extern "C" void PrintDebug(const char* msg){
    spdlog::debug(msg);
}


 extern "C" void PrintInfoWithArgs(const char* msg, int arg){
    spdlog::info(msg, arg);
}


 extern "C" void PrintInfoWithArgs(const char* format, ...){
    va_list args;
    va_start(args, format);
    
    spdlog::info(fmt::vformat(format, fmt::make_format_args(args)));
    va_end(args);
}

