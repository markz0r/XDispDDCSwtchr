#ifndef XDISPDDCSWTCHR_BRIDGE_DARWIN_H
#define XDISPDDCSWTCHR_BRIDGE_DARWIN_H

#include <stdint.h>

#define XDISP_MAX_EDID 1024
#define XDISP_MAX_NAME 128
#define XDISP_MAX_PATH 1024
#define XDISP_MAX_ERROR 512
#define XDISP_MAX_DIAGNOSTIC 8192

typedef struct {
    uint32_t display_id;
    uint64_t service_registry_id;
    uint32_t vendor;
    uint32_t model;
    uint32_t serial;
    uint32_t edid_length;
    uint8_t edid[XDISP_MAX_EDID];
    char name[XDISP_MAX_NAME];
    char display_location[XDISP_MAX_PATH];
    char service_path[XDISP_MAX_PATH];
} xdisp_display_info;

typedef void *xdisp_ddc_handle;

int xdispddcswtchr_symbol_probe(char error[XDISP_MAX_ERROR]);
int xdispddcswtchr_discovery_probe(char output[XDISP_MAX_DIAGNOSTIC], char error[XDISP_MAX_ERROR]);
int xdispddcswtchr_display_count(uint32_t *count, char error[XDISP_MAX_ERROR]);
int xdispddcswtchr_display_identity(uint32_t index, xdisp_display_info *info, char error[XDISP_MAX_ERROR]);
int xdispddcswtchr_ddc_open(uint64_t service_registry_id, xdisp_ddc_handle *handle, char error[XDISP_MAX_ERROR]);
int xdispddcswtchr_ddc_transaction(
    xdisp_ddc_handle handle,
    const uint8_t *request,
    uint32_t request_length,
    uint8_t *reply,
    uint32_t reply_length,
    uint32_t reply_delay_microseconds,
    int32_t *native_status,
    char error[XDISP_MAX_ERROR]
);
void xdispddcswtchr_ddc_close(xdisp_ddc_handle handle);

#endif
