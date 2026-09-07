package mods

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// Constant pool tags from the JVM Specification (Java 25, chapter 4.4).
// If a future JVM version introduces a new tag, parsing must fail loudly
// rather than silently misinterpreting the pool. There is deliberately no
// version check: as long as all tags are known, newer class files parse fine.
const (
	cpTagUtf8               = 1
	cpTagInteger            = 3
	cpTagFloat              = 4
	cpTagLong               = 5
	cpTagDouble             = 6
	cpTagClass              = 7
	cpTagString             = 8
	cpTagFieldref           = 9
	cpTagMethodref          = 10
	cpTagInterfaceMethodref = 11
	cpTagNameAndType        = 12
	cpTagMethodHandle       = 15
	cpTagMethodType         = 16
	cpTagDynamic            = 17
	cpTagInvokeDynamic      = 18
	cpTagModule             = 19
	cpTagPackage            = 20
)

// Attribute names that carry annotations (JVM Specification chapter 4.7).
const (
	attrRuntimeVisibleAnnotations          = "RuntimeVisibleAnnotations"
	attrRuntimeInvisibleAnnotations        = "RuntimeInvisibleAnnotations"
	attrRuntimeVisibleParameterAnnotations = "RuntimeVisibleParameterAnnotations"
	attrRuntimeInvisibleParameterAnnot     = "RuntimeInvisibleParameterAnnotations"
	attrRuntimeVisibleTypeAnnotations      = "RuntimeVisibleTypeAnnotations"
	attrRuntimeInvisibleTypeAnnotations    = "RuntimeInvisibleTypeAnnotations"
	attrAnnotationDefault                  = "AnnotationDefault"
)

// ClassInfo holds the information extracted from a single Java class file.
type ClassInfo struct {
	// Name is the fully qualified internal name of the declared class
	// (this_class), e.g. "com/example/Foo" or "com/example/Foo$Bar".
	Name string
	// References lists the internal class names the class file references:
	// CONSTANT_Class entries in the constant pool and class values in
	// annotations (element values tagged 'c', e.g. @Ref(Foo.class)). Self
	// references and vendored-package names are not filtered here. Array
	// descriptors ("[Lcom/x/Foo;") are unwrapped. Deduplicated, unordered.
	References []string
}

// ParseClassFile parses a Java class file and extracts the declared class
// name and all class references (constant pool entries and annotation class
// values). Fields, methods and attributes are walked only as far as needed to
// reach the annotation attributes; all other attribute contents are skipped.
//
// The parser errors on unknown constant pool tags and unknown annotation
// element value tags so that class files from future JVM versions are never
// misinterpreted. No bytecode version check is performed.
func ParseClassFile(data []byte) (ClassInfo, error) {
	r := &classReader{data: data}

	magic, err := r.u4()
	if err != nil {
		return ClassInfo{}, fmt.Errorf("reading magic number: %w", err)
	}
	if magic != 0xCAFEBABE {
		return ClassInfo{}, fmt.Errorf("bad magic number 0x%08X (expected 0xCAFEBABE)", magic)
	}

	// minor_version and major_version are read for stream positioning but
	// deliberately not validated.
	if _, err := r.u2(); err != nil {
		return ClassInfo{}, fmt.Errorf("reading minor version: %w", err)
	}
	if _, err := r.u2(); err != nil {
		return ClassInfo{}, fmt.Errorf("reading major version: %w", err)
	}

	cpCount, err := r.u2()
	if err != nil {
		return ClassInfo{}, fmt.Errorf("reading constant pool count: %w", err)
	}
	if cpCount == 0 {
		return ClassInfo{}, fmt.Errorf("invalid constant pool count 0")
	}

	// utf8s and classes are indexed by constant pool index. Entries may
	// reference any other entry, including later ones, so names are resolved
	// in a second pass after the whole pool has been read.
	utf8s := make([]string, cpCount)
	type classEntry struct{ nameIndex uint16 }
	classes := make([]classEntry, cpCount)

	for i := uint16(1); i < cpCount; i++ {
		tag, err := r.u1()
		if err != nil {
			return ClassInfo{}, fmt.Errorf("reading constant pool tag at index %d: %w", i, err)
		}
		switch tag {
		case cpTagUtf8:
			length, err := r.u2()
			if err != nil {
				return ClassInfo{}, fmt.Errorf("reading Utf8 length at index %d: %w", i, err)
			}
			raw, err := r.bytes(int(length))
			if err != nil {
				return ClassInfo{}, fmt.Errorf("reading Utf8 content at index %d: %w", i, err)
			}
			utf8s[i] = string(raw)
		case cpTagInteger, cpTagFloat:
			if err := r.skip(4); err != nil {
				return ClassInfo{}, fmt.Errorf("reading constant at index %d: %w", i, err)
			}
		case cpTagLong, cpTagDouble:
			if err := r.skip(8); err != nil {
				return ClassInfo{}, fmt.Errorf("reading constant at index %d: %w", i, err)
			}
			// 8-byte constants take up two constant pool slots.
			i++
		case cpTagClass:
			nameIndex, err := r.u2()
			if err != nil {
				return ClassInfo{}, fmt.Errorf("reading Class name index at index %d: %w", i, err)
			}
			classes[i] = classEntry{nameIndex: nameIndex}
		case cpTagString:
			if err := r.skip(2); err != nil {
				return ClassInfo{}, fmt.Errorf("reading String constant at index %d: %w", i, err)
			}
		case cpTagFieldref, cpTagMethodref, cpTagInterfaceMethodref:
			if err := r.skip(4); err != nil {
				return ClassInfo{}, fmt.Errorf("reading member reference at index %d: %w", i, err)
			}
		case cpTagNameAndType:
			if err := r.skip(4); err != nil {
				return ClassInfo{}, fmt.Errorf("reading NameAndType at index %d: %w", i, err)
			}
		case cpTagMethodHandle:
			if err := r.skip(3); err != nil {
				return ClassInfo{}, fmt.Errorf("reading MethodHandle at index %d: %w", i, err)
			}
		case cpTagMethodType:
			if err := r.skip(2); err != nil {
				return ClassInfo{}, fmt.Errorf("reading MethodType at index %d: %w", i, err)
			}
		case cpTagDynamic, cpTagInvokeDynamic:
			if err := r.skip(4); err != nil {
				return ClassInfo{}, fmt.Errorf("reading dynamic constant at index %d: %w", i, err)
			}
		case cpTagModule, cpTagPackage:
			if err := r.skip(2); err != nil {
				return ClassInfo{}, fmt.Errorf("reading Module/Package constant at index %d: %w", i, err)
			}
		default:
			return ClassInfo{}, fmt.Errorf(
				"unknown constant pool tag %d (0x%02X) at index %d, byte offset %d; class file uses features this parser does not support",
				tag, tag, i, r.offset-1)
		}
	}

	// access_flags: not used for filtering — all declared classes are indexed
	// regardless of visibility (public, protected, package-private, private).
	if _, err := r.u2(); err != nil {
		return ClassInfo{}, fmt.Errorf("reading access flags: %w", err)
	}

	thisClass, err := r.u2()
	if err != nil {
		return ClassInfo{}, fmt.Errorf("reading this_class: %w", err)
	}
	thisClassEntry := classEntry{}
	if thisClass != 0 && thisClass < cpCount {
		thisClassEntry = classes[thisClass]
	}
	nameIndex := thisClassEntry.nameIndex
	if nameIndex == 0 || int(nameIndex) >= int(cpCount) {
		return ClassInfo{}, fmt.Errorf("this_class index %d does not point to a CONSTANT_Class entry", thisClass)
	}
	name := utf8s[nameIndex]
	if name == "" {
		return ClassInfo{}, fmt.Errorf("this_class name index %d does not point to a Utf8 constant", nameIndex)
	}

	ctx := &classParseContext{
		r:          r,
		utf8s:      utf8s,
		cpCount:    cpCount,
		className:  name,
		references: make(map[string]struct{}),
	}

	// CONSTANT_Class entries: class literals (ldc), member references,
	// instanceof/checkcast, etc.
	for _, entry := range classes {
		if entry.nameIndex == 0 || entry.nameIndex >= cpCount {
			continue
		}
		if ref := classReferenceName(utf8s[entry.nameIndex]); ref != "" {
			ctx.addReference(ref)
		}
	}

	if err := ctx.readClassTail(); err != nil {
		return ClassInfo{}, err
	}

	info := ClassInfo{Name: name}
	if len(ctx.references) > 0 {
		info.References = make([]string, 0, len(ctx.references))
		for ref := range ctx.references {
			info.References = append(info.References, ref)
		}
	}
	return info, nil
}

// classParseContext carries the state needed to resolve references while
// walking the part of the class file after this_class.
type classParseContext struct {
	r          *classReader
	utf8s      []string
	cpCount    uint16
	className  string
	references map[string]struct{}
}

// utf8 returns the Utf8 constant at the given index, or "" when the index is
// out of bounds or does not point to a Utf8 constant.
func (c *classParseContext) utf8(index uint16) string {
	if index == 0 || index >= c.cpCount {
		return ""
	}
	return c.utf8s[index]
}

// addReference records a class reference unless it is empty or the class
// naming itself.
func (c *classParseContext) addReference(internalName string) {
	if internalName == "" || internalName == c.className {
		return
	}
	c.references[internalName] = struct{}{}
}

// readClassTail parses super_class, interfaces, fields, methods and class
// attributes. Only the annotation attributes among them are inspected.
func (c *classParseContext) readClassTail() error {
	if err := c.r.skip(2); err != nil { // super_class
		return fmt.Errorf("reading super_class: %w", err)
	}
	interfacesCount, err := c.r.u2()
	if err != nil {
		return fmt.Errorf("reading interfaces count: %w", err)
	}
	if err := c.r.skip(int(interfacesCount) * 2); err != nil {
		return fmt.Errorf("reading interfaces: %w", err)
	}

	fieldsCount, err := c.r.u2()
	if err != nil {
		return fmt.Errorf("reading fields count: %w", err)
	}
	for i := uint16(0); i < fieldsCount; i++ {
		if err := c.readMemberInfo(); err != nil {
			return fmt.Errorf("reading field %d: %w", i, err)
		}
	}

	methodsCount, err := c.r.u2()
	if err != nil {
		return fmt.Errorf("reading methods count: %w", err)
	}
	for i := uint16(0); i < methodsCount; i++ {
		if err := c.readMemberInfo(); err != nil {
			return fmt.Errorf("reading method %d: %w", i, err)
		}
	}

	if err := c.readAttributes(); err != nil {
		return fmt.Errorf("reading class attributes: %w", err)
	}
	return nil
}

// readMemberInfo parses a field_info or method_info structure: access flags,
// name, descriptor and the attribute table.
func (c *classParseContext) readMemberInfo() error {
	if err := c.r.skip(6); err != nil { // access_flags, name_index, descriptor_index
		return fmt.Errorf("reading member header: %w", err)
	}
	return c.readAttributes()
}

// readAttributes parses an attributes_count-prefixed attribute table. Only
// annotation-carrying attributes are decoded; every other attribute's content
// is skipped via its declared length.
func (c *classParseContext) readAttributes() error {
	count, err := c.r.u2()
	if err != nil {
		return fmt.Errorf("reading attributes count: %w", err)
	}
	for i := uint16(0); i < count; i++ {
		nameIndex, err := c.r.u2()
		if err != nil {
			return fmt.Errorf("reading attribute name index: %w", err)
		}
		length, err := c.r.u4()
		if err != nil {
			return fmt.Errorf("reading attribute length: %w", err)
		}
		end := c.r.offset + int(length)
		if int(length) < 0 || end > len(c.r.data) {
			return fmt.Errorf("attribute %q (length %d) exceeds class file bounds at offset %d", c.utf8(nameIndex), length, c.r.offset)
		}

		name := c.utf8(nameIndex)
		if name == "Code" {
			// Code bodies carry their own attribute table, which is where
			// javac stores type annotations on local variables, catch
			// clauses, instanceof checks, casts and method references.
			if err := c.readCodeAttribute(); err != nil {
				return fmt.Errorf("reading Code attribute: %w", err)
			}
		} else if isAnnotationAttribute(name) {
			if err := c.readAnnotationAttribute(name, end); err != nil {
				return err
			}
		}
		// Skip whatever was not consumed (or the whole attribute for
		// uninteresting names). A desynchronized annotation parse therefore
		// cannot poison the following attributes.
		c.r.offset = end
	}
	return nil
}

// readCodeAttribute parses the Code attribute's header (max_stack, max_locals,
// bytecode, exception table) so its own attribute table — carrying code-body
// type annotations — can be walked via readAttributes.
func (c *classParseContext) readCodeAttribute() error {
	if err := c.r.skip(4); err != nil { // max_stack, max_locals
		return fmt.Errorf("reading max_stack/max_locals: %w", err)
	}
	codeLength, err := c.r.u4()
	if err != nil {
		return fmt.Errorf("reading code length: %w", err)
	}
	if int(codeLength) < 0 || c.r.offset+int(codeLength) > len(c.r.data) {
		return fmt.Errorf("code length %d exceeds class file bounds at offset %d", codeLength, c.r.offset)
	}
	if err := c.r.skip(int(codeLength)); err != nil {
		return fmt.Errorf("skipping code: %w", err)
	}
	etableLength, err := c.r.u2()
	if err != nil {
		return fmt.Errorf("reading exception table length: %w", err)
	}
	if err := c.r.skip(int(etableLength) * 8); err != nil { // start, end, handler, catch_type each
		return fmt.Errorf("skipping exception table: %w", err)
	}
	return c.readAttributes()
}

func isAnnotationAttribute(name string) bool {
	switch name {
	case attrRuntimeVisibleAnnotations,
		attrRuntimeInvisibleAnnotations,
		attrRuntimeVisibleParameterAnnotations,
		attrRuntimeInvisibleParameterAnnot,
		attrRuntimeVisibleTypeAnnotations,
		attrRuntimeInvisibleTypeAnnotations,
		attrAnnotationDefault:
		return true
	}
	return false
}

// readAnnotationAttribute decodes an annotation-carrying attribute. end is
// the byte offset just past the attribute, bounding the parse.
func (c *classParseContext) readAnnotationAttribute(name string, end int) error {
	switch name {
	case attrRuntimeVisibleAnnotations, attrRuntimeInvisibleAnnotations:
		count, err := c.r.u2()
		if err != nil {
			return fmt.Errorf("reading %s count: %w", name, err)
		}
		for i := uint16(0); i < count; i++ {
			if err := c.readAnnotation(); err != nil {
				return fmt.Errorf("reading %s annotation %d: %w", name, i, err)
			}
		}
	case attrRuntimeVisibleParameterAnnotations, attrRuntimeInvisibleParameterAnnot:
		numParams, err := c.r.u1()
		if err != nil {
			return fmt.Errorf("reading %s parameter count: %w", name, err)
		}
		for p := uint8(0); p < numParams; p++ {
			count, err := c.r.u2()
			if err != nil {
				return fmt.Errorf("reading %s annotation count: %w", name, err)
			}
			for i := uint16(0); i < count; i++ {
				if err := c.readAnnotation(); err != nil {
					return fmt.Errorf("reading %s annotation: %w", name, err)
				}
			}
		}
	case attrRuntimeVisibleTypeAnnotations, attrRuntimeInvisibleTypeAnnotations:
		count, err := c.r.u2()
		if err != nil {
			return fmt.Errorf("reading %s count: %w", name, err)
		}
		for i := uint16(0); i < count; i++ {
			if err := c.readTypeTargetInfo(); err != nil {
				return fmt.Errorf("reading %s target info: %w", name, err)
			}
			if err := c.readTypePath(); err != nil {
				return fmt.Errorf("reading %s type path: %w", name, err)
			}
			if err := c.readAnnotation(); err != nil {
				return fmt.Errorf("reading %s annotation: %w", name, err)
			}
		}
	case attrAnnotationDefault:
		if err := c.readElementValue(); err != nil {
			return fmt.Errorf("reading %s: %w", name, err)
		}
	}
	return nil
}

// readTypeTargetInfo skips a type annotation's target_info structure (JVM
// Specification chapter 4.7.20.1). The variant is selected by target_type.
func (c *classParseContext) readTypeTargetInfo() error {
	targetType, err := c.r.u1()
	if err != nil {
		return fmt.Errorf("reading target_type: %w", err)
	}
	switch targetType {
	// type_parameter_target: u1 type_parameter_index
	case 0x00, 0x01:
		err = c.r.skip(1)
	// supertype_target: u2 supertype_index
	case 0x10:
		err = c.r.skip(2)
	// type_parameter_bound_target: u1 type_parameter_index, u1 bound_index
	case 0x11, 0x12:
		err = c.r.skip(2)
	// empty_target: 0x13 field, 0x14 method return, 0x15 method receiver
	case 0x13, 0x14, 0x15:
	// formal_parameter_target: u1 formal_parameter_index
	case 0x16:
		err = c.r.skip(1)
	// throws_target: u2 throws_type_index
	case 0x17:
		err = c.r.skip(2)
	// localvar_target: u2 table_length, then table_length * (u2 start_pc,
	// u2 length, u2 index)
	case 0x40, 0x41:
		length, lenErr := c.r.u2()
		if lenErr != nil {
			return fmt.Errorf("reading local var target table length: %w", lenErr)
		}
		err = c.r.skip(int(length) * 6)
	// catch_target: u2 exception_table_index
	case 0x42:
		err = c.r.skip(2)
	// offset_target: u2 offset (instanceof, method reference new/identity, cast)
	case 0x43, 0x44, 0x45, 0x46:
		err = c.r.skip(2)
	// type_argument_target: u2 offset, u1 type_argument_index
	case 0x47, 0x48, 0x49, 0x4A, 0x4B:
		err = c.r.skip(3)
	default:
		return fmt.Errorf("unknown type annotation target_type 0x%02X", targetType)
	}
	if err != nil {
		return fmt.Errorf("reading target info: %w", err)
	}
	return nil
}

// readTypePath skips a type annotation's type_path structure.
func (c *classParseContext) readTypePath() error {
	length, err := c.r.u1()
	if err != nil {
		return fmt.Errorf("reading type path length: %w", err)
	}
	return c.r.skip(int(length) * 2) // array index / nested type step each
}

// readAnnotation parses an annotation structure and records the annotation's
// own type (the JVM must resolve it to read the annotation via reflection) as
// well as its element values.
func (c *classParseContext) readAnnotation() error {
	typeIndex, err := c.r.u2()
	if err != nil {
		return fmt.Errorf("reading annotation type: %w", err)
	}
	c.addReference(internalNameFromDescriptor(c.utf8(typeIndex)))
	numPairs, err := c.r.u2()
	if err != nil {
		return fmt.Errorf("reading element value pair count: %w", err)
	}
	for i := uint16(0); i < numPairs; i++ {
		if err := c.r.skip(2); err != nil { // element_name_index
			return fmt.Errorf("reading element name: %w", err)
		}
		if err := c.readElementValue(); err != nil {
			return fmt.Errorf("reading element value: %w", err)
		}
	}
	return nil
}

// readElementValue parses an element_value (JVM Specification chapter
// 4.7.16.1) and records class references from 'c' (class) values. Nested
// annotations ('@') and arrays ('[') are handled recursively.
func (c *classParseContext) readElementValue() error {
	tag, err := c.r.u1()
	if err != nil {
		return fmt.Errorf("reading element value tag: %w", err)
	}
	switch tag {
	case 'B', 'C', 'D', 'F', 'I', 'J', 'S', 'Z', 's': // primitive/String constant
		err = c.r.skip(2)
	case 'e': // enum constant: type_name and const_name
		err = c.r.skip(4)
	case 'c': // class: a field descriptor like "Lcom/x/Foo;" or "[Lcom/x/Foo;"
		nameIndex, idxErr := c.r.u2()
		if idxErr != nil {
			return fmt.Errorf("reading class info index: %w", idxErr)
		}
		c.addReference(internalNameFromDescriptor(c.utf8(nameIndex)))
	case '@': // nested annotation
		return c.readAnnotation()
	case '[': // array of element values
		length, lenErr := c.r.u2()
		if lenErr != nil {
			return fmt.Errorf("reading array length: %w", lenErr)
		}
		for i := uint16(0); i < length; i++ {
			if err := c.readElementValue(); err != nil {
				return fmt.Errorf("reading array element %d: %w", i, err)
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown annotation element value tag %q (0x%02X)", string(tag), tag)
	}
	if err != nil {
		return fmt.Errorf("reading element value %q: %w", string(tag), err)
	}
	return nil
}

// classReferenceName normalizes a CONSTANT_Class-derived name into a bare
// internal class name ("com/x/Foo"). Array class references like
// "[Lcom/x/Foo;" unwrap to "com/x/Foo"; primitive arrays ("[I") yield "".
func classReferenceName(nameOrDescriptor string) string {
	if !strings.HasPrefix(nameOrDescriptor, "[") {
		return nameOrDescriptor
	}
	return internalNameFromDescriptor(nameOrDescriptor)
}

// internalNameFromDescriptor unwraps a field descriptor that names a class:
// "Lcom/x/Foo;" → "com/x/Foo", "[Lcom/x/Foo;" → "com/x/Foo". Primitive and
// other non-class descriptors yield "".
func internalNameFromDescriptor(desc string) string {
	for len(desc) > 0 && desc[0] == '[' {
		desc = desc[1:]
	}
	if len(desc) >= 3 && desc[0] == 'L' && desc[len(desc)-1] == ';' {
		return desc[1 : len(desc)-1]
	}
	return ""
}

// classReader is a minimal big-endian reader over a byte slice.
type classReader struct {
	data   []byte
	offset int
}

func (r *classReader) u1() (uint8, error) {
	b, err := r.bytes(1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

func (r *classReader) u2() (uint16, error) {
	b, err := r.bytes(2)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(b), nil
}

func (r *classReader) u4() (uint32, error) {
	b, err := r.bytes(4)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(b), nil
}

func (r *classReader) bytes(n int) ([]byte, error) {
	if n < 0 || r.offset+n > len(r.data) {
		return nil, fmt.Errorf("unexpected end of class file: need %d bytes at offset %d, have %d", n, r.offset, len(r.data)-r.offset)
	}
	b := r.data[r.offset : r.offset+n]
	r.offset += n
	return b, nil
}

func (r *classReader) skip(n int) error {
	_, err := r.bytes(n)
	return err
}
