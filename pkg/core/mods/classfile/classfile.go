package classfile

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// Constant pool tags from the JVM Specification.
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

// ClassInfo holds information extracted from a single Java class file.
type ClassInfo struct {
	// Name is the fully qualified internal name of the declared class (this_class),
	// e.g. "com/example/Foo".
	Name string
	// References lists unique internal class names referenced in the constant pool
	// and annotations. Array descriptors ("[Lcom/x/Foo;") are unwrapped.
	References []string
}

// ParseClassFile parses a Java class file, extracting the declared class name
// and all referenced class names.
func ParseClassFile(data []byte) (ClassInfo, error) {
	r := &classReader{data: data}

	magic, err := r.u4()
	if err != nil {
		return ClassInfo{}, fmt.Errorf("reading magic number: %w", err)
	}
	if magic != 0xCAFEBABE {
		return ClassInfo{}, fmt.Errorf("bad magic number 0x%08X", magic)
	}

	// Skip minor and major version.
	if err := r.skip(4); err != nil {
		return ClassInfo{}, fmt.Errorf("reading version: %w", err)
	}

	cpCount, err := r.u2()
	if err != nil {
		return ClassInfo{}, fmt.Errorf("reading constant pool count: %w", err)
	}
	if cpCount == 0 {
		return ClassInfo{}, fmt.Errorf("invalid constant pool count 0")
	}

	utf8s := make([]string, cpCount)
	type classEntry struct{ nameIndex uint16 }
	classes := make([]classEntry, cpCount)
	var descriptorIndices []uint16

	for i := uint16(1); i < cpCount; i++ {
		tag, err := r.u1()
		if err != nil {
			return ClassInfo{}, fmt.Errorf("reading constant pool tag at %d: %w", i, err)
		}
		switch tag {
		case cpTagUtf8:
			length, err := r.u2()
			if err != nil {
				return ClassInfo{}, fmt.Errorf("reading Utf8 length at %d: %w", i, err)
			}
			raw, err := r.bytes(int(length))
			if err != nil {
				return ClassInfo{}, fmt.Errorf("reading Utf8 content at %d: %w", i, err)
			}
			utf8s[i] = string(raw)
		case cpTagInteger, cpTagFloat:
			if err := r.skip(4); err != nil {
				return ClassInfo{}, fmt.Errorf("skipping 4-byte constant at %d: %w", i, err)
			}
		case cpTagLong, cpTagDouble:
			if err := r.skip(8); err != nil {
				return ClassInfo{}, fmt.Errorf("skipping 8-byte constant at %d: %w", i, err)
			}
			i++ // 8-byte constants consume two pool slots.
		case cpTagClass:
			nameIndex, err := r.u2()
			if err != nil {
				return ClassInfo{}, fmt.Errorf("reading Class name index at %d: %w", i, err)
			}
			classes[i] = classEntry{nameIndex: nameIndex}
		case cpTagString:
			if err := r.skip(2); err != nil {
				return ClassInfo{}, fmt.Errorf("skipping String at %d: %w", i, err)
			}
		case cpTagFieldref, cpTagMethodref, cpTagInterfaceMethodref:
			if err := r.skip(4); err != nil {
				return ClassInfo{}, fmt.Errorf("skipping member ref at %d: %w", i, err)
			}
		case cpTagNameAndType:
			if err := r.skip(2); err != nil {
				return ClassInfo{}, fmt.Errorf("reading NameAndType name index at %d: %w", i, err)
			}
			descriptorIndex, err := r.u2()
			if err != nil {
				return ClassInfo{}, fmt.Errorf("reading NameAndType descriptor index at %d: %w", i, err)
			}
			descriptorIndices = append(descriptorIndices, descriptorIndex)
		case cpTagMethodHandle:
			if err := r.skip(3); err != nil {
				return ClassInfo{}, fmt.Errorf("skipping MethodHandle at %d: %w", i, err)
			}
		case cpTagMethodType:
			descriptorIndex, err := r.u2()
			if err != nil {
				return ClassInfo{}, fmt.Errorf("reading MethodType descriptor index at %d: %w", i, err)
			}
			descriptorIndices = append(descriptorIndices, descriptorIndex)
		case cpTagDynamic, cpTagInvokeDynamic:
			if err := r.skip(4); err != nil {
				return ClassInfo{}, fmt.Errorf("skipping dynamic constant at %d: %w", i, err)
			}
		case cpTagModule, cpTagPackage:
			if err := r.skip(2); err != nil {
				return ClassInfo{}, fmt.Errorf("skipping Module/Package at %d: %w", i, err)
			}
		default:
			return ClassInfo{}, fmt.Errorf("unknown constant pool tag %d at %d", tag, i)
		}
	}

	if err := r.skip(2); err != nil { // access_flags
		return ClassInfo{}, fmt.Errorf("skipping access flags: %w", err)
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
		return ClassInfo{}, fmt.Errorf("this_class index %d does not point to Class entry", thisClass)
	}
	name := utf8s[nameIndex]
	if name == "" {
		return ClassInfo{}, fmt.Errorf("this_class name index %d is empty", nameIndex)
	}

	ctx := &classParseContext{
		r:          r,
		utf8s:      utf8s,
		cpCount:    cpCount,
		className:  name,
		references: make(map[string]struct{}),
	}

	for _, entry := range classes {
		if entry.nameIndex == 0 || entry.nameIndex >= cpCount {
			continue
		}
		ctx.addClassReference(utf8s[entry.nameIndex])
	}
	for _, descriptorIndex := range descriptorIndices {
		ctx.addDescriptorReferences(ctx.utf8(descriptorIndex))
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

type classParseContext struct {
	r          *classReader
	utf8s      []string
	cpCount    uint16
	className  string
	references map[string]struct{}
}

func (c *classParseContext) utf8(index uint16) string {
	if index == 0 || index >= c.cpCount {
		return ""
	}
	return c.utf8s[index]
}

func (c *classParseContext) addReference(internalName string) {
	if internalName == "" || internalName == c.className {
		return
	}
	c.references[internalName] = struct{}{}
}

func (c *classParseContext) addClassReference(nameOrDescriptor string) {
	if strings.HasPrefix(nameOrDescriptor, "[") {
		c.addDescriptorReferences(nameOrDescriptor)
		return
	}
	c.addReference(nameOrDescriptor)
}

// addDescriptorReferences extracts every object type from a field, method, or
// generic signature descriptor. A descriptor may contain multiple types, such
// as "(Lpkg/A;[Lpkg/B;)Lpkg/C;".
func (c *classParseContext) addDescriptorReferences(descriptor string) {
	for i := 0; i < len(descriptor); i++ {
		if descriptor[i] != 'L' {
			continue
		}
		semicolon := strings.IndexByte(descriptor[i+1:], ';')
		if semicolon < 0 {
			continue
		}
		end := i + 1 + semicolon
		if generic := strings.IndexByte(descriptor[i+1:], '<'); generic >= 0 && i+1+generic < end {
			end = i + 1 + generic
		}
		if end > i+1 {
			c.addReference(descriptor[i+1 : end])
		}
		i = end
	}
}

func internalNameFromDescriptor(desc string) string {
	for len(desc) > 0 && desc[0] == '[' {
		desc = desc[1:]
	}
	if len(desc) >= 3 && desc[0] == 'L' && desc[len(desc)-1] == ';' {
		return desc[1 : len(desc)-1]
	}
	return ""
}

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
