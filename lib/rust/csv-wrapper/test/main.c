#include <stdio.h>
#include "../csv_wrapper.h"

int main() {
    void* reader = csv_reader_new("data.csv");
    if (reader != NULL) {
        const char* record;
        while ((record = csv_reader_read_record(reader)) != NULL) {
            printf("%s", record);
            free_string((char*)record);
        }
        csv_reader_free(reader);
    } else {
        printf("Failed to create CSV reader.\n");
    }
    return 0;
}
