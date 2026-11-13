//go:build !dot && !spot && !panel

package camera

import (
	"runtime"
	"testing"
	"unsafe"
)

// The imgsensor and ION ioctl arguments as the MT8163's 4.9 kernel lays them out. A 64-bit daemon
// goes through unlocked_ioctl with the native (LP64) structs, where every pointer and size_t is eight
// bytes and an embedded {int handle; struct ion_handle *kernel_handle} union is eight bytes, eight
// aligned; a 32-bit one goes through compat_ioctl with the ILP32 structs. The ioctl number carries the
// struct's size, so the kernel refuses a request whose size is the other ABI's.
//
// Sources (drivers/misc/mediatek/imgsensor/inc/kd_imgsensor_define_default.h, drivers/staging/android/
// uapi/ion.h, drivers/staging/android/ion/mtk/ion_drv.h):
//
//	struct ACDK_SENSOR_CONTROL_STRUCT        { u32 InvokeCamera; enum ScenarioId; void *pImageWindow; void *pSensorConfigData; }
//	struct ACDK_SENSOR_FEATURECONTROL_STRUCT { u32 InvokeCamera; enum FeatureId; u8 *pFeaturePara; u32 *pFeatureParaLen; }
//	struct ion_allocation_data               { size_t len; size_t align; u32 heap_id_mask; u32 flags; int handle; }
//	struct ion_custom_data                   { u32 cmd; unsigned long arg; }
//	struct ion_sys_get_phys_param            { union { int handle; void *kernel_handle; }; u32 phy_addr; unsigned long len; }
//	struct ion_mm_config_buffer_param        { union { int handle; void *kernel_handle; }; int module_id; u32 security, coherent, reserve_iova_start, reserve_iova_end; }
//	struct ion_sys_data                      { enum sys_cmd; union { ...; struct ion_sys_get_phys_param get_phys_param; ... }; }
//	struct ion_mm_data                       { enum mm_cmd; union { struct ion_mm_config_buffer_param config_buffer_param; ... }; }
func TestCameraIoctlStructSizesAndCodes(t *testing.T) {
	type layout struct {
		name      string
		got, want uintptr
	}
	var checks []layout
	var codes []layout
	if unsafe.Sizeof(uintptr(0)) == 8 {
		checks = []layout{
			{"sizeof(sensControl)", unsafe.Sizeof(sensControl{}), 24},
			{"offsetof(sensControl.WindowP)", unsafe.Offsetof(sensControl{}.WindowP), 8},
			{"offsetof(sensControl.ConfigP)", unsafe.Offsetof(sensControl{}.ConfigP), 16},
			{"sizeof(sensFeature)", unsafe.Sizeof(sensFeature{}), 24},
			{"offsetof(sensFeature.ParaP)", unsafe.Offsetof(sensFeature{}.ParaP), 8},
			{"offsetof(sensFeature.ParaLenP)", unsafe.Offsetof(sensFeature{}.ParaLenP), 16},
			{"sizeof(ionAlloc)", unsafe.Sizeof(ionAlloc{}), 32},
			{"offsetof(ionAlloc.HeapIDMask)", unsafe.Offsetof(ionAlloc{}.HeapIDMask), 16},
			{"offsetof(ionAlloc.Handle)", unsafe.Offsetof(ionAlloc{}.Handle), 24},
			{"sizeof(ionCustom)", unsafe.Sizeof(ionCustom{}), 16},
			{"offsetof(ionCustom.Arg)", unsafe.Offsetof(ionCustom{}.Arg), 8},
			{"sizeof(ionSysGetPhysP)", unsafe.Sizeof(ionSysGetPhysP{}), 24},
			{"offsetof(ionSysGetPhysP.PhyAddr)", unsafe.Offsetof(ionSysGetPhysP{}.PhyAddr), 8},
			{"offsetof(ionSysGetPhysP.Len)", unsafe.Offsetof(ionSysGetPhysP{}.Len), 16},
			{"offsetof(ionSysData.Phys)", unsafe.Offsetof(ionSysData{}.Phys), 8},
			{"sizeof(ionMMConfig)", unsafe.Sizeof(ionMMConfig{}), 32},
			{"offsetof(ionMMConfig.ModuleID)", unsafe.Offsetof(ionMMConfig{}.ModuleID), 8},
			{"offsetof(ionMMData.Config)", unsafe.Offsetof(ionMMData{}.Config), 8},
		}
		codes = []layout{
			{"KDIMGSENSORIOC_X_CONTROL", reqSensControl, 0xC0186914},
			{"KDIMGSENSORIOC_X_FEATURECONCTROL", reqSensFeature, 0xC018690F},
			{"ION_IOC_ALLOC", reqIonAlloc, 0xC0204900},
			{"ION_IOC_SHARE", reqIonShare, 0xC0084904},
			{"ION_IOC_CUSTOM", reqIonCustom, 0xC0104906},
		}
	} else {
		checks = []layout{
			{"sizeof(sensControl)", unsafe.Sizeof(sensControl{}), 16},
			{"offsetof(sensControl.WindowP)", unsafe.Offsetof(sensControl{}.WindowP), 8},
			{"offsetof(sensControl.ConfigP)", unsafe.Offsetof(sensControl{}.ConfigP), 12},
			{"sizeof(sensFeature)", unsafe.Sizeof(sensFeature{}), 16},
			{"offsetof(sensFeature.ParaP)", unsafe.Offsetof(sensFeature{}.ParaP), 8},
			{"offsetof(sensFeature.ParaLenP)", unsafe.Offsetof(sensFeature{}.ParaLenP), 12},
			{"sizeof(ionAlloc)", unsafe.Sizeof(ionAlloc{}), 20},
			{"offsetof(ionAlloc.HeapIDMask)", unsafe.Offsetof(ionAlloc{}.HeapIDMask), 8},
			{"offsetof(ionAlloc.Handle)", unsafe.Offsetof(ionAlloc{}.Handle), 16},
			{"sizeof(ionCustom)", unsafe.Sizeof(ionCustom{}), 8},
			{"offsetof(ionCustom.Arg)", unsafe.Offsetof(ionCustom{}.Arg), 4},
			{"sizeof(ionSysGetPhysP)", unsafe.Sizeof(ionSysGetPhysP{}), 12},
			{"offsetof(ionSysGetPhysP.PhyAddr)", unsafe.Offsetof(ionSysGetPhysP{}.PhyAddr), 4},
			{"offsetof(ionSysGetPhysP.Len)", unsafe.Offsetof(ionSysGetPhysP{}.Len), 8},
			{"offsetof(ionSysData.Phys)", unsafe.Offsetof(ionSysData{}.Phys), 4},
			{"sizeof(ionMMConfig)", unsafe.Sizeof(ionMMConfig{}), 24},
			{"offsetof(ionMMConfig.ModuleID)", unsafe.Offsetof(ionMMConfig{}.ModuleID), 4},
			{"offsetof(ionMMData.Config)", unsafe.Offsetof(ionMMData{}.Config), 4},
		}
		codes = []layout{
			{"KDIMGSENSORIOC_X_CONTROL", reqSensControl, 0xC0106914},
			{"KDIMGSENSORIOC_X_FEATURECONCTROL", reqSensFeature, 0xC010690F},
			{"ION_IOC_ALLOC", reqIonAlloc, 0xC0144900},
			{"ION_IOC_SHARE", reqIonShare, 0xC0084904},
			{"ION_IOC_CUSTOM", reqIonCustom, 0xC0084906},
		}
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
	for _, c := range codes {
		if c.got != c.want {
			t.Errorf("%s = %#x, want %#x", c.name, c.got, c.want)
		}
	}
	// The kernel copies its whole ion_sys_data / ion_mm_data in and back out: ours must be at least
	// as large, or the copy back writes past the object (LP64: 120 and 80 bytes; ILP32 compat: 52
	// and 72).
	sysMin, mmMin := uintptr(52), uintptr(72)
	if unsafe.Sizeof(uintptr(0)) == 8 {
		sysMin, mmMin = 120, 80
	}
	if s := unsafe.Sizeof(ionSysData{}); s < sysMin {
		t.Errorf("sizeof(ionSysData) = %d, the kernel copies back %d", s, sysMin)
	}
	if s := unsafe.Sizeof(ionMMData{}); s < mmMin {
		t.Errorf("sizeof(ionMMData) = %d, the kernel copies back %d", s, mmMin)
	}
}

// pinAddr hands the kernel the address of a pinned object and keeps it pinned until the Pinner lets go.
func TestPinAddrIsTheObjectsAddress(t *testing.T) {
	var p runtime.Pinner
	defer p.Unpin()
	v := new([256]byte)
	if got := pinAddr(&p, v); got != uintptr(unsafe.Pointer(v)) {
		t.Fatalf("pinAddr = %#x, want %#x", got, uintptr(unsafe.Pointer(v)))
	}
}
