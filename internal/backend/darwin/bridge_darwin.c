//go:build darwin && arm64

#include "bridge_darwin.h"

#include <CoreFoundation/CoreFoundation.h>
#include <CoreGraphics/CoreGraphics.h>
#include <IOKit/IOKitLib.h>
#include <dlfcn.h>
#include <pthread.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

typedef CFTypeRef (*ioav_create_fn)(CFAllocatorRef, io_service_t);
typedef CFTypeRef (*ioav_create_default_fn)(CFAllocatorRef);
typedef CFTypeRef (*ioav_create_location_fn)(CFAllocatorRef, uint32_t);
typedef int32_t (*ioav_read_fn)(CFTypeRef, uint32_t, uint32_t, void *, uint32_t);
typedef int32_t (*ioav_write_fn)(CFTypeRef, uint32_t, uint32_t, const void *, uint32_t);
typedef CFDictionaryRef (*display_info_fn)(CGDirectDisplayID);

typedef struct {
    CFTypeRef service;
} xdisp_handle_impl;

typedef struct {
    CGDirectDisplayID display_id;
    CFDataRef edid;
    char name[XDISP_MAX_NAME];
    char location[XDISP_MAX_PATH];
} cg_display_info;

typedef struct {
    io_service_t io_service;
    CFTypeRef av_service;
    CFDataRef edid;
    uint64_t registry_id;
    char path[XDISP_MAX_PATH];
} av_display_info;

static void *core_display_handle;
static ioav_create_fn ioav_create;
static ioav_create_default_fn ioav_create_default;
static ioav_create_location_fn ioav_create_location;
static ioav_read_fn ioav_read;
static ioav_write_fn ioav_write;
static display_info_fn display_create_info;
static int symbols_available;
static char symbol_error[XDISP_MAX_ERROR];
static pthread_once_t symbols_once = PTHREAD_ONCE_INIT;

static void release_av_displays(av_display_info *items, uint32_t count);

static void set_error(char *error, const char *message) {
    if (error == NULL) {
        return;
    }
    snprintf(error, XDISP_MAX_ERROR, "%s", message == NULL ? "unknown error" : message);
}

static void initialize_symbols(void) {
    core_display_handle = dlopen("/System/Library/Frameworks/CoreDisplay.framework/CoreDisplay", RTLD_LAZY | RTLD_LOCAL);
    if (core_display_handle == NULL) {
        snprintf(symbol_error, sizeof(symbol_error), "%s", dlerror());
        return;
    }
    ioav_create = (ioav_create_fn)dlsym(core_display_handle, "IOAVServiceCreateWithService");
    ioav_create_default = (ioav_create_default_fn)dlsym(core_display_handle, "IOAVServiceCreate");
    ioav_create_location = (ioav_create_location_fn)dlsym(core_display_handle, "IOAVServiceCreateWithLocation");
    ioav_read = (ioav_read_fn)dlsym(core_display_handle, "IOAVServiceReadI2C");
    ioav_write = (ioav_write_fn)dlsym(core_display_handle, "IOAVServiceWriteI2C");
    display_create_info = (display_info_fn)dlsym(core_display_handle, "CoreDisplay_DisplayCreateInfoDictionary");
    if (ioav_create == NULL || ioav_read == NULL || ioav_write == NULL || display_create_info == NULL) {
        snprintf(symbol_error, sizeof(symbol_error), "%s", "one or more required CoreDisplay symbols are unavailable");
        return;
    }
    symbols_available = 1;
}

static int load_symbols(char *error) {
    if (pthread_once(&symbols_once, initialize_symbols) != 0) {
        set_error(error, "CoreDisplay symbol initialization failed");
        return -2;
    }
    if (!symbols_available) {
        set_error(error, symbol_error[0] == '\0' ? "required CoreDisplay symbols are unavailable" : symbol_error);
        return -2;
    }
    return 0;
}

int xdispddcswtchr_symbol_probe(char error[XDISP_MAX_ERROR]) {
    return load_symbols(error);
}

static void copy_cf_string(CFStringRef value, char *out, size_t out_size) {
    if (value == NULL || out == NULL || out_size == 0) {
        return;
    }
    if (!CFStringGetCString(value, out, (CFIndex)out_size, kCFStringEncodingUTF8)) {
        out[0] = '\0';
    }
}

static void product_name(CFDictionaryRef dictionary, char out[XDISP_MAX_NAME]) {
    CFTypeRef names_value = CFDictionaryGetValue(dictionary, CFSTR("DisplayProductName"));
    if (names_value == NULL || CFGetTypeID(names_value) != CFDictionaryGetTypeID()) {
        return;
    }
    CFDictionaryRef names = (CFDictionaryRef)names_value;
    CFIndex count = CFDictionaryGetCount(names);
    if (count <= 0) {
        return;
    }
    const void **keys = calloc((size_t)count, sizeof(void *));
    const void **values = calloc((size_t)count, sizeof(void *));
    if (keys == NULL || values == NULL) {
        free(keys);
        free(values);
        return;
    }
    CFDictionaryGetKeysAndValues(names, keys, values);
    if (values[0] != NULL && CFGetTypeID(values[0]) == CFStringGetTypeID()) {
        copy_cf_string((CFStringRef)values[0], out, XDISP_MAX_NAME);
    }
    free(keys);
    free(values);
}

static int collect_cg_displays(cg_display_info **result, uint32_t *result_count, char *error) {
    uint32_t count = 0;
    CGError cg_error = CGGetOnlineDisplayList(0, NULL, &count);
    if (cg_error != kCGErrorSuccess) {
        set_error(error, "CGGetOnlineDisplayList count failed");
        return -3;
    }
    CGDirectDisplayID *ids = calloc(count, sizeof(CGDirectDisplayID));
    cg_display_info *items = calloc(count, sizeof(cg_display_info));
    if ((count > 0 && ids == NULL) || (count > 0 && items == NULL)) {
        free(ids);
        free(items);
        set_error(error, "allocate CoreGraphics display list failed");
        return -4;
    }
    cg_error = CGGetOnlineDisplayList(count, ids, &count);
    if (cg_error != kCGErrorSuccess) {
        free(ids);
        free(items);
        set_error(error, "CGGetOnlineDisplayList failed");
        return -3;
    }
    uint32_t external_count = 0;
    for (uint32_t i = 0; i < count; i++) {
        if (CGDisplayIsBuiltin(ids[i])) {
            continue;
        }
        CFDictionaryRef info = display_create_info(ids[i]);
        if (info == NULL) {
            continue;
        }
        CFTypeRef edid_value = CFDictionaryGetValue(info, CFSTR("IODisplayEDIDOriginal"));
        if (edid_value == NULL) {
            edid_value = CFDictionaryGetValue(info, CFSTR("IODisplayEDID"));
        }
        if (edid_value != NULL && CFGetTypeID(edid_value) == CFDataGetTypeID()) {
            items[external_count].edid = CFRetain((CFDataRef)edid_value);
        }
        CFTypeRef location_value = CFDictionaryGetValue(info, CFSTR("IODisplayLocation"));
        if (location_value != NULL && CFGetTypeID(location_value) == CFStringGetTypeID()) {
            copy_cf_string((CFStringRef)location_value, items[external_count].location, XDISP_MAX_PATH);
        }
        product_name(info, items[external_count].name);
        items[external_count].display_id = ids[i];
        external_count++;
        CFRelease(info);
    }
    free(ids);
    *result = items;
    *result_count = external_count;
    return 0;
}

static int edid_checksum_valid(const uint8_t *edid, CFIndex length) {
    if (edid == NULL || length < 128) {
        return 0;
    }
    uint8_t sum = 0;
    for (int i = 0; i < 128; i++) {
        sum = (uint8_t)(sum + edid[i]);
    }
    return sum == 0;
}

static CFDataRef read_service_edid(CFTypeRef service) {
    uint8_t buffer[128] = {0};
    int32_t status = ioav_read(service, 0x50, 0, buffer, sizeof(buffer));
    if (status != 0 || !edid_checksum_valid(buffer, sizeof(buffer))) {
        return NULL;
    }
    return CFDataCreate(kCFAllocatorDefault, buffer, sizeof(buffer));
}

static int collect_av_displays(av_display_info **result, uint32_t *result_count, char *error) {
    io_iterator_t iterator = IO_OBJECT_NULL;
    kern_return_t status = IOServiceGetMatchingServices(kIOMainPortDefault, IOServiceMatching("DCPAVServiceProxy"), &iterator);
    if (status != KERN_SUCCESS) {
        set_error(error, "enumerating DCPAVServiceProxy services failed");
        return -5;
    }
    uint32_t capacity = 4;
    uint32_t count = 0;
    av_display_info *items = calloc(capacity, sizeof(av_display_info));
    if (items == NULL) {
        IOObjectRelease(iterator);
        set_error(error, "allocate DCP service list failed");
        return -4;
    }
    io_service_t service;
    while ((service = IOIteratorNext(iterator)) != IO_OBJECT_NULL) {
        CFTypeRef location = IORegistryEntryCreateCFProperty(service, CFSTR("Location"), kCFAllocatorDefault, 0);
        int external = location != NULL && CFGetTypeID(location) == CFStringGetTypeID() && CFStringCompare((CFStringRef)location, CFSTR("External"), 0) == kCFCompareEqualTo;
        if (location != NULL) {
            CFRelease(location);
        }
        if (!external) {
            IOObjectRelease(service);
            continue;
        }
        if (count == capacity) {
            capacity *= 2;
            av_display_info *grown = realloc(items, capacity * sizeof(av_display_info));
            if (grown == NULL) {
                IOObjectRelease(service);
                IOObjectRelease(iterator);
                release_av_displays(items, count);
                set_error(error, "grow DCP service list failed");
                return -4;
            }
            items = grown;
            memset(items + count, 0, (capacity - count) * sizeof(av_display_info));
        }
        CFTypeRef av_service = ioav_create(kCFAllocatorDefault, service);
        if (av_service == NULL) {
            IOObjectRelease(service);
            continue;
        }
        items[count].io_service = service;
        items[count].av_service = av_service;
        items[count].edid = read_service_edid(av_service);
        IORegistryEntryGetRegistryEntryID(service, &items[count].registry_id);
        io_string_t path = {0};
        if (IORegistryEntryGetPath(service, kIOServicePlane, path) == KERN_SUCCESS) {
            snprintf(items[count].path, XDISP_MAX_PATH, "%s", path);
        }
        count++;
    }
    IOObjectRelease(iterator);
    *result = items;
    *result_count = count;
    return 0;
}

static void release_cg_displays(cg_display_info *items, uint32_t count) {
    if (items == NULL) return;
    for (uint32_t i = 0; i < count; i++) {
        if (items[i].edid != NULL) CFRelease(items[i].edid);
    }
    free(items);
}

static void release_av_displays(av_display_info *items, uint32_t count) {
    if (items == NULL) return;
    for (uint32_t i = 0; i < count; i++) {
        if (items[i].edid != NULL) CFRelease(items[i].edid);
        if (items[i].av_service != NULL) CFRelease(items[i].av_service);
        if (items[i].io_service != IO_OBJECT_NULL) IOObjectRelease(items[i].io_service);
    }
    free(items);
}

static int edid_matches(CFDataRef left, CFDataRef right) {
    if (left == NULL || right == NULL || CFDataGetLength(left) < 128 || CFDataGetLength(right) < 128) {
        return 0;
    }
    return memcmp(CFDataGetBytePtr(left), CFDataGetBytePtr(right), 128) == 0;
}

static uint16_t read_u16_be(const uint8_t *data) {
    return (uint16_t)(((uint16_t)data[0] << 8) | data[1]);
}

static uint16_t read_u16_le(const uint8_t *data) {
    return (uint16_t)(((uint16_t)data[1] << 8) | data[0]);
}

static uint32_t read_u32_le(const uint8_t *data) {
    return ((uint32_t)data[3] << 24) | ((uint32_t)data[2] << 16) | ((uint32_t)data[1] << 8) | data[0];
}

static int edid_identity_matches(const cg_display_info *display, CFDataRef edid) {
    if (display == NULL || edid == NULL || CFDataGetLength(edid) < 16) {
        return 0;
    }
    const uint8_t *bytes = CFDataGetBytePtr(edid);
    uint32_t vendor = CGDisplayVendorNumber(display->display_id);
    uint32_t model = CGDisplayModelNumber(display->display_id);
    uint32_t serial = CGDisplaySerialNumber(display->display_id);
    if (read_u16_be(bytes + 8) != vendor || read_u16_le(bytes + 10) != model) {
        return 0;
    }
    uint32_t edid_serial = read_u32_le(bytes + 12);
    return serial == 0 || edid_serial == 0 || serial == edid_serial;
}

static int location_matches(const char *display_location, const char *service_path) {
    if (display_location == NULL || service_path == NULL || display_location[0] == '\0' || service_path[0] == '\0') {
        return 0;
    }
    size_t length = strlen(display_location);
    if (strncmp(display_location, service_path, length) != 0) {
        return 0;
    }
    return service_path[length] == '\0' || service_path[length] == '/';
}

static void append_diagnostic(char *output, size_t output_size, const char *format, ...) {
    if (output == NULL || output_size == 0) return;
    size_t used = strnlen(output, output_size);
    if (used >= output_size - 1) return;
    va_list args;
    va_start(args, format);
    vsnprintf(output + used, output_size - used, format, args);
    va_end(args);
}

int xdispddcswtchr_discovery_probe(char output[XDISP_MAX_DIAGNOSTIC], char error[XDISP_MAX_ERROR]) {
    if (output == NULL) {
        set_error(error, "diagnostic output pointer is null");
        return -1;
    }
    output[0] = '\0';
    int status = load_symbols(error);
    if (status != 0) return status;

    Dl_info symbol_info = {0};
    if (dladdr((void *)ioav_create, &symbol_info) != 0) {
        append_diagnostic(output, XDISP_MAX_DIAGNOSTIC, "ioav_create_image=%s\n", symbol_info.dli_fname == NULL ? "" : symbol_info.dli_fname);
    }
    CFTypeRef default_service = ioav_create_default == NULL ? NULL : ioav_create_default(kCFAllocatorDefault);
    CFTypeRef location_service = ioav_create_location == NULL ? NULL : ioav_create_location(kCFAllocatorDefault, 0);
    append_diagnostic(output, XDISP_MAX_DIAGNOSTIC, "ioav_default=%s ioav_location_external=%s\n",
        default_service == NULL ? "null" : "created", location_service == NULL ? "null" : "created");
    if (default_service != NULL) CFRelease(default_service);
    if (location_service != NULL) CFRelease(location_service);

    uint32_t raw_cg_count = 0;
    CGError raw_cg_status = CGGetOnlineDisplayList(0, NULL, &raw_cg_count);
    append_diagnostic(output, XDISP_MAX_DIAGNOSTIC, "cg_status=%d cg_online=%u\n", raw_cg_status, raw_cg_count);
    if (raw_cg_status == kCGErrorSuccess && raw_cg_count > 0) {
        CGDirectDisplayID *raw_ids = calloc(raw_cg_count, sizeof(CGDirectDisplayID));
        if (raw_ids != NULL && CGGetOnlineDisplayList(raw_cg_count, raw_ids, &raw_cg_count) == kCGErrorSuccess) {
            for (uint32_t i = 0; i < raw_cg_count; i++) {
                append_diagnostic(output, XDISP_MAX_DIAGNOSTIC, "cg_raw[%u] id=%u builtin=%d vendor=%u model=%u serial=%u\n",
                    i, raw_ids[i], CGDisplayIsBuiltin(raw_ids[i]), CGDisplayVendorNumber(raw_ids[i]),
                    CGDisplayModelNumber(raw_ids[i]), CGDisplaySerialNumber(raw_ids[i]));
            }
        }
        free(raw_ids);
    }

    io_iterator_t raw_iterator = IO_OBJECT_NULL;
    kern_return_t raw_io_status = IOServiceGetMatchingServices(kIOMainPortDefault, IOServiceMatching("DCPAVServiceProxy"), &raw_iterator);
    append_diagnostic(output, XDISP_MAX_DIAGNOSTIC, "dcp_match_status=0x%x\n", raw_io_status);
    if (raw_io_status == KERN_SUCCESS) {
        uint32_t raw_index = 0;
        io_service_t raw_service;
        while ((raw_service = IOIteratorNext(raw_iterator)) != IO_OBJECT_NULL) {
            char raw_location[XDISP_MAX_NAME] = {0};
            CFTypeRef location = IORegistryEntryCreateCFProperty(raw_service, CFSTR("Location"), kCFAllocatorDefault, 0);
            CFTypeID type_id = location == NULL ? 0 : CFGetTypeID(location);
            if (location != NULL && type_id == CFStringGetTypeID()) {
                copy_cf_string((CFStringRef)location, raw_location, sizeof(raw_location));
            }
            uint64_t registry_id = 0;
            IORegistryEntryGetRegistryEntryID(raw_service, &registry_id);
            CFTypeRef raw_av_service = ioav_create(kCFAllocatorDefault, raw_service);
            append_diagnostic(output, XDISP_MAX_DIAGNOSTIC, "dcp_raw[%u] registry_id=0x%llx location_type=%lu location=%s av_service=%s\n",
                raw_index, (unsigned long long)registry_id, (unsigned long)type_id, raw_location,
                raw_av_service == NULL ? "null" : "created");
            if (raw_av_service != NULL) CFRelease(raw_av_service);
            if (location != NULL) CFRelease(location);
            IOObjectRelease(raw_service);
            raw_index++;
        }
        IOObjectRelease(raw_iterator);
    }

    cg_display_info *cg_items = NULL;
    av_display_info *av_items = NULL;
    uint32_t cg_count = 0;
    uint32_t av_count = 0;
    status = collect_cg_displays(&cg_items, &cg_count, error);
    if (status != 0) return status;
    status = collect_av_displays(&av_items, &av_count, error);
    if (status != 0) {
        release_cg_displays(cg_items, cg_count);
        return status;
    }
    append_diagnostic(output, XDISP_MAX_DIAGNOSTIC, "coregraphics_external=%u dcp_external=%u\n", cg_count, av_count);
    for (uint32_t i = 0; i < cg_count; i++) {
        append_diagnostic(output, XDISP_MAX_DIAGNOSTIC,
            "cg[%u] id=%u name=%s edid_bytes=%ld location=%s\n", i,
            cg_items[i].display_id, cg_items[i].name,
            cg_items[i].edid == NULL ? 0L : (long)CFDataGetLength(cg_items[i].edid),
            cg_items[i].location);
    }
    for (uint32_t i = 0; i < av_count; i++) {
        append_diagnostic(output, XDISP_MAX_DIAGNOSTIC,
            "dcp[%u] registry_id=0x%llx edid_bytes=%ld path=%s\n", i,
            (unsigned long long)av_items[i].registry_id,
            av_items[i].edid == NULL ? 0L : (long)CFDataGetLength(av_items[i].edid),
            av_items[i].path);
    }
    release_cg_displays(cg_items, cg_count);
    release_av_displays(av_items, av_count);
    return 0;
}

static int enumerate_one(uint32_t requested_index, xdisp_display_info *out, uint32_t *total, char *error) {
    int status = load_symbols(error);
    if (status != 0) return status;
    cg_display_info *cg_items = NULL;
    av_display_info *av_items = NULL;
    uint32_t cg_count = 0;
    uint32_t av_count = 0;
    status = collect_cg_displays(&cg_items, &cg_count, error);
    if (status != 0) return status;
    status = collect_av_displays(&av_items, &av_count, error);
    if (status != 0) {
        release_cg_displays(cg_items, cg_count);
        return status;
    }

    int *cg_matches = cg_count == 0 ? NULL : malloc(cg_count * sizeof(int));
    uint32_t *av_owners = av_count == 0 ? NULL : calloc(av_count, sizeof(uint32_t));
    if ((cg_count > 0 && cg_matches == NULL) || (av_count > 0 && av_owners == NULL)) {
        free(cg_matches);
        free(av_owners);
        release_cg_displays(cg_items, cg_count);
        release_av_displays(av_items, av_count);
        set_error(error, "allocate display correlation state failed");
        return -4;
    }

    int ambiguous = 0;
    for (uint32_t i = 0; i < cg_count; i++) {
        cg_matches[i] = -1;
        uint32_t candidate_count = 0;
        int candidate = -1;
        for (uint32_t j = 0; j < av_count; j++) {
            if (!edid_matches(cg_items[i].edid, av_items[j].edid) &&
                !edid_identity_matches(&cg_items[i], av_items[j].edid) &&
                !location_matches(cg_items[i].location, av_items[j].path)) continue;
            candidate = (int)j;
            candidate_count++;
        }
        if (candidate_count > 1) {
            ambiguous = 1;
        } else if (candidate_count == 1) {
            cg_matches[i] = candidate;
            av_owners[candidate]++;
        }
    }
    for (uint32_t j = 0; j < av_count; j++) {
        if (av_owners[j] > 1) ambiguous = 1;
    }
    if (ambiguous) {
        free(cg_matches);
        free(av_owners);
        release_cg_displays(cg_items, cg_count);
        release_av_displays(av_items, av_count);
        set_error(error, "display-to-DDC service correlation is ambiguous");
        return -9;
    }

    uint32_t matched_count = 0;
    int result = -6;
    for (uint32_t i = 0; i < cg_count; i++) {
        int matched_av = cg_matches[i];
        if (matched_av < 0) continue;
        if (out != NULL && matched_count == requested_index) {
            memset(out, 0, sizeof(*out));
            out->display_id = cg_items[i].display_id;
            out->service_registry_id = av_items[matched_av].registry_id;
            out->vendor = CGDisplayVendorNumber(cg_items[i].display_id);
            out->model = CGDisplayModelNumber(cg_items[i].display_id);
            out->serial = CGDisplaySerialNumber(cg_items[i].display_id);
            snprintf(out->name, XDISP_MAX_NAME, "%s", cg_items[i].name);
            snprintf(out->display_location, XDISP_MAX_PATH, "%s", cg_items[i].location);
            snprintf(out->service_path, XDISP_MAX_PATH, "%s", av_items[matched_av].path);
            CFDataRef matched_edid = cg_items[i].edid == NULL ? av_items[matched_av].edid : cg_items[i].edid;
            if (matched_edid != NULL) {
                CFIndex length = CFDataGetLength(matched_edid);
                if (length > XDISP_MAX_EDID) length = XDISP_MAX_EDID;
                memcpy(out->edid, CFDataGetBytePtr(matched_edid), (size_t)length);
                out->edid_length = (uint32_t)length;
            }
            result = 0;
        }
        matched_count++;
    }
    if (total != NULL) *total = matched_count;
    if (out == NULL) result = 0;
    if (out != NULL && requested_index >= matched_count) {
        set_error(error, "display index is out of range or no EDID-correlated DCP service was found");
        result = -6;
    }
    free(cg_matches);
    free(av_owners);
    release_cg_displays(cg_items, cg_count);
    release_av_displays(av_items, av_count);
    return result;
}

int xdispddcswtchr_display_count(uint32_t *count, char error[XDISP_MAX_ERROR]) {
    if (count == NULL) {
        set_error(error, "count pointer is null");
        return -1;
    }
    *count = 0;
    return enumerate_one(0, NULL, count, error);
}

int xdispddcswtchr_display_identity(uint32_t index, xdisp_display_info *info, char error[XDISP_MAX_ERROR]) {
    if (info == NULL) {
        set_error(error, "display info pointer is null");
        return -1;
    }
    return enumerate_one(index, info, NULL, error);
}

int xdispddcswtchr_ddc_open(uint64_t service_registry_id, xdisp_ddc_handle *handle, char error[XDISP_MAX_ERROR]) {
    if (handle == NULL) {
        set_error(error, "handle pointer is null");
        return -1;
    }
    *handle = NULL;
    int status = load_symbols(error);
    if (status != 0) return status;
    CFMutableDictionaryRef matching = IORegistryEntryIDMatching(service_registry_id);
    if (matching == NULL) {
        set_error(error, "could not create registry ID match");
        return -6;
    }
    io_service_t service = IOServiceGetMatchingService(kIOMainPortDefault, matching);
    if (service == IO_OBJECT_NULL) {
        set_error(error, "DCP service is no longer present");
        return -6;
    }
    CFTypeRef av_service = ioav_create(kCFAllocatorDefault, service);
    IOObjectRelease(service);
    if (av_service == NULL) {
        set_error(error, "IOAVServiceCreateWithService failed");
        return -7;
    }
    xdisp_handle_impl *impl = calloc(1, sizeof(xdisp_handle_impl));
    if (impl == NULL) {
        CFRelease(av_service);
        set_error(error, "allocate DDC handle failed");
        return -4;
    }
    impl->service = av_service;
    *handle = impl;
    return 0;
}

int xdispddcswtchr_ddc_transaction(
    xdisp_ddc_handle handle,
    const uint8_t *request,
    uint32_t request_length,
    uint8_t *reply,
    uint32_t reply_length,
    uint32_t reply_delay_microseconds,
    int32_t *native_status,
    char error[XDISP_MAX_ERROR]
) {
    if (handle == NULL || request == NULL || request_length < 2 || request[0] != 0x51) {
        set_error(error, "invalid DDC transaction arguments");
        return -1;
    }
    xdisp_handle_impl *impl = (xdisp_handle_impl *)handle;
    int32_t status = ioav_write(impl->service, 0x37, request[0], request + 1, request_length - 1);
    if (native_status != NULL) *native_status = status;
    if (status != 0) {
        snprintf(error, XDISP_MAX_ERROR, "IOAVServiceWriteI2C failed with status %d", status);
        return -8;
    }
    if (reply_length == 0) return 0;
    if (reply == NULL) {
        set_error(error, "reply pointer is null");
        return -1;
    }
    if (reply_delay_microseconds > 0) usleep(reply_delay_microseconds);
    memset(reply, 0, reply_length);
    status = ioav_read(impl->service, 0x37, 0, reply, reply_length);
    if (native_status != NULL) *native_status = status;
    if (status != 0) {
        snprintf(error, XDISP_MAX_ERROR, "IOAVServiceReadI2C failed with status %d", status);
        return -8;
    }
    return 0;
}

void xdispddcswtchr_ddc_close(xdisp_ddc_handle handle) {
    if (handle == NULL) return;
    xdisp_handle_impl *impl = (xdisp_handle_impl *)handle;
    if (impl->service != NULL) CFRelease(impl->service);
    free(impl);
}
