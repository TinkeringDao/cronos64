//go:build (arm64 || amd64 || riscv64 || loong64 || ppc64 || ppc64le || mips64 || mips64le || s390x) && !dot && !spot && !panel

package camera

// The imgsensor and ION ioctl arguments for a 64-bit daemon: the kernel's native (LP64) structs, read
// through unlocked_ioctl. Pointers, size_t and unsigned long are eight bytes, and MediaTek's
// {int handle; struct ion_handle *kernel_handle} union is eight bytes, eight aligned, with the user
// handle in its low half. ioctl_layout_test.go has the C declarations these follow.

// sensControl is ACDK_SENSOR_CONTROL_STRUCT (KDIMGSENSORIOC_X_CONTROL).
type sensControl struct {
	InvokeIdx, ScenarioId uint32
	WindowP, ConfigP      uintptr
}

// sensFeature is ACDK_SENSOR_FEATURECONTROL_STRUCT (KDIMGSENSORIOC_X_FEATURECONCTROL).
type sensFeature struct {
	InvokeIdx, FeatureId uint32
	ParaP, ParaLenP      uintptr
}

// ionAlloc is ion_allocation_data (ION_IOC_ALLOC): 32 bytes, the last four the struct's tail padding.
type ionAlloc struct {
	Len, Align        uintptr
	HeapIDMask, Flags uint32
	Handle            int32
	_                 int32
}

// ionCustom is ion_custom_data (ION_IOC_CUSTOM).
type ionCustom struct {
	Cmd uint32
	_   uint32
	Arg uintptr
}

// ionSysGetPhysP is ion_sys_get_phys_param: the handle union, then the device address at 8 and the
// length at 16. Inside ion_sys_data it starts at 8, the union's alignment.
type ionSysGetPhysP struct {
	Handle  int32
	_       uint32 // kernel_handle's upper half
	PhyAddr uint32
	Len     uintptr
}

// ionMMConfig is ion_mm_config_buffer_param: the handle union, then module_id at 8. The leading
// zero-length field gives it the C struct's eight-byte alignment, so it sits at 8 inside ion_mm_data
// and is 32 bytes long.
type ionMMConfig struct {
	_                                                [0]uintptr
	Handle                                           int32
	_                                                uint32 // kernel_handle's upper half
	ModuleID, Security, Coherent, IovaStart, IovaEnd uint32
}
