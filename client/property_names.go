package client

// Common Android system property names usable with Client.GetProp and
// Client.Properties. Android property availability and meaning can vary by OS
// version, vendor image, build type, and target device.
const (
	// PropProductBrand is the consumer-visible product brand.
	PropProductBrand = "ro.product.brand"

	// PropProductManufacturer is the device manufacturer.
	PropProductManufacturer = "ro.product.manufacturer"

	// PropProductModel is the consumer-visible product model.
	PropProductModel = "ro.product.model"

	// PropProductName is the product name for the current build.
	PropProductName = "ro.product.name"

	// PropProductDevice is the product device codename for the current build.
	PropProductDevice = "ro.product.device"

	// PropProductBoard is the device board name.
	PropProductBoard = "ro.product.board"

	// PropProductCPUABI is the primary application binary interface.
	PropProductCPUABI = "ro.product.cpu.abi"

	// PropProductCPUABIList is the comma-separated list of supported ABIs.
	PropProductCPUABIList = "ro.product.cpu.abilist"

	// PropBuildFingerprint is the unique build fingerprint string.
	PropBuildFingerprint = "ro.build.fingerprint"

	// PropBuildID is the build ID.
	PropBuildID = "ro.build.id"

	// PropBuildDisplayID is the human-readable build display ID.
	PropBuildDisplayID = "ro.build.display.id"

	// PropBuildType is the build type, such as user, userdebug, or eng.
	PropBuildType = "ro.build.type"

	// PropBuildTags is the build tag list, such as release-keys or test-keys.
	PropBuildTags = "ro.build.tags"

	// PropBuildVersionRelease is the Android release version.
	PropBuildVersionRelease = "ro.build.version.release"

	// PropBuildVersionSDK is the Android SDK/API level.
	PropBuildVersionSDK = "ro.build.version.sdk"

	// PropBuildVersionIncremental is the incremental build version.
	PropBuildVersionIncremental = "ro.build.version.incremental"

	// PropBuildVersionSecurityPatch is the Android security patch level.
	PropBuildVersionSecurityPatch = "ro.build.version.security_patch"

	// PropHardware is the hardware name reported by the device build.
	PropHardware = "ro.hardware"

	// PropBootloader is the bootloader version.
	PropBootloader = "ro.bootloader"

	// PropSerialNo is the device serial number property when exposed by the image.
	PropSerialNo = "ro.serialno"

	// PropDebuggable reports whether the build is debuggable (usually "0" or "1").
	PropDebuggable = "ro.debuggable"

	// PropSecure reports whether the build runs adb in secure mode (usually "0" or "1").
	PropSecure = "ro.secure"
)
