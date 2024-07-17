#include "spdlog/spdlog.h"

extern "C" {

void PrintInfo(const char* msg){
    spdlog::info(msg);
}

void PrintError(const char* msg){
    spdlog::error(msg);
}

void PrintWarn(const char* msg){
    spdlog::warn(msg);
}

void PrintCritical(const char* msg){
    spdlog::critical(msg);
}

void PrintDebug(const char* msg){
    spdlog::debug(msg);
}

void PrintInfoWithInt(const char* msg, int arg){
    spdlog::info(msg, arg);
}

}