//go:build (arm || 386 || mips || mipsle) && !dot && !spot && !panel

package camera

// The imgsensor and ION ioctl arguments for a 32-bit daemon on the 64-bit kernel: the compat (ILP32)
// structs, which compat_ioctl widens into the native ones. Pointers, size_t and unsigned long are four
// bytes, and so is the {int handle; struct ion_handle *kernel_handle} union. ioctl_layout_test.go has
// the C declarations these follow.

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

// ionAlloc is ion_allocation_data (ION_IOC_ALLOC): 20 bytes.
type ionAlloc struct {
	Len, Align        uintptr
	HeapIDMask, Flags uint32
	Handle            int32
}

// ionCustom is ion_custom_data (ION_IOC_CUSTOM).
type ionCustom struct {
	Cmd uint32
	Arg uintptr
}

// ionSysGetPhysP is compat_ion_sys_get_phys_param; at 4 inside ion_sys_data.
type ionSysGetPhysP struct {
	Handle  int32
	PhyAddr uint32
	Len     uintptr
}

// ionMMConfig is compat_ion_mm_config_buf_param; at 4 inside ion_mm_data.
type ionMMConfig struct {
	Handle                                           int32
	ModuleID, Security, Coherent, IovaStart, IovaEnd uint32
}
