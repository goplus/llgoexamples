#include "spdlog/spdlog.h"

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

 extern "C" void PrintInfoWithInt(const char* msg, int arg){
    spdlog::info(msg, arg);
}