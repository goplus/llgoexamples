#ifndef CSV_WRAPPER_H
#define CSV_WRAPPER_H

#include <stdarg.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>

/**
 * Create a new CSV reader for the specified file path.
 */
void *csv_reader_new(const char *file_path);

/**
 * Free the memory allocated for the CSV reader.
 */
void csv_reader_free(void *ptr);

/**
 * Read the next record from the CSV reader and return it as a C string.
 */
const char *csv_reader_read_record(void *ptr);

/**
 * Free the memory allocated for a C string returned by other functions.
 */
void free_string(char *s);

#endif /* CSV_WRAPPER_H */
