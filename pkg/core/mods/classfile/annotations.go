package classfile

import (
	"fmt"
)

const (
	attrRuntimeVisibleAnnotations          = "RuntimeVisibleAnnotations"
	attrRuntimeInvisibleAnnotations        = "RuntimeInvisibleAnnotations"
	attrRuntimeVisibleParameterAnnotations = "RuntimeVisibleParameterAnnotations"
	attrRuntimeInvisibleParameterAnnot     = "RuntimeInvisibleParameterAnnotations"
	attrRuntimeVisibleTypeAnnotations      = "RuntimeVisibleTypeAnnotations"
	attrRuntimeInvisibleTypeAnnotations    = "RuntimeInvisibleTypeAnnotations"
	attrAnnotationDefault                  = "AnnotationDefault"
)

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

func (c *classParseContext) readMemberInfo() error {
	if err := c.r.skip(2); err != nil { // access_flags
		return fmt.Errorf("reading member header: %w", err)
	}
	if err := c.r.skip(2); err != nil { // name_index
		return fmt.Errorf("reading member name index: %w", err)
	}
	descriptorIndex, err := c.r.u2()
	if err != nil {
		return fmt.Errorf("reading member descriptor index: %w", err)
	}
	c.addDescriptorReferences(c.utf8(descriptorIndex))
	return c.readAttributes()
}

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
			return fmt.Errorf("attribute %q (length %d) exceeds bounds", c.utf8(nameIndex), length)
		}

		name := c.utf8(nameIndex)
		if name == "Code" {
			if err := c.readCodeAttribute(); err != nil {
				return fmt.Errorf("reading Code attribute: %w", err)
			}
		} else if name == "Signature" {
			if length < 2 {
				return fmt.Errorf("attribute %q length %d is invalid (must be 2)", name, length)
			}
			signatureIndex, err := c.r.u2()
			if err != nil {
				return fmt.Errorf("reading Signature attribute: %w", err)
			}
			c.addDescriptorReferences(c.utf8(signatureIndex))
		} else if isAnnotationAttribute(name) {
			if err := c.readAnnotationAttribute(name, end); err != nil {
				return err
			}
		}
		if c.r.offset > end {
			return fmt.Errorf("attribute %q consumed %d bytes exceeding declared length %d", name, c.r.offset-(end-int(length)), length)
		}
		c.r.offset = end
	}
	return nil
}

func (c *classParseContext) readCodeAttribute() error {
	if err := c.r.skip(4); err != nil { // max_stack, max_locals
		return fmt.Errorf("reading max_stack/max_locals: %w", err)
	}
	codeLength, err := c.r.u4()
	if err != nil {
		return fmt.Errorf("reading code length: %w", err)
	}
	if int(codeLength) < 0 || c.r.offset+int(codeLength) > len(c.r.data) {
		return fmt.Errorf("code length %d exceeds bounds", codeLength)
	}
	if err := c.r.skip(int(codeLength)); err != nil {
		return fmt.Errorf("skipping code: %w", err)
	}
	etableLength, err := c.r.u2()
	if err != nil {
		return fmt.Errorf("reading exception table length: %w", err)
	}
	if err := c.r.skip(int(etableLength) * 8); err != nil {
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

func (c *classParseContext) readTypeTargetInfo() error {
	targetType, err := c.r.u1()
	if err != nil {
		return fmt.Errorf("reading target_type: %w", err)
	}
	switch targetType {
	case 0x00, 0x01:
		err = c.r.skip(1)
	case 0x10:
		err = c.r.skip(2)
	case 0x11, 0x12:
		err = c.r.skip(2)
	case 0x13, 0x14, 0x15:
	case 0x16:
		err = c.r.skip(1)
	case 0x17:
		err = c.r.skip(2)
	case 0x40, 0x41:
		length, lenErr := c.r.u2()
		if lenErr != nil {
			return fmt.Errorf("reading local var target table length: %w", lenErr)
		}
		err = c.r.skip(int(length) * 6)
	case 0x42:
		err = c.r.skip(2)
	case 0x43, 0x44, 0x45, 0x46:
		err = c.r.skip(2)
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

func (c *classParseContext) readTypePath() error {
	length, err := c.r.u1()
	if err != nil {
		return fmt.Errorf("reading type path length: %w", err)
	}
	return c.r.skip(int(length) * 2)
}

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
		if err := c.r.skip(2); err != nil {
			return fmt.Errorf("reading element name: %w", err)
		}
		if err := c.readElementValue(); err != nil {
			return fmt.Errorf("reading element value: %w", err)
		}
	}
	return nil
}

func (c *classParseContext) readElementValue() error {
	tag, err := c.r.u1()
	if err != nil {
		return fmt.Errorf("reading element value tag: %w", err)
	}
	switch tag {
	case 'B', 'C', 'D', 'F', 'I', 'J', 'S', 'Z', 's':
		err = c.r.skip(2)
	case 'e':
		typeNameIndex, idxErr := c.r.u2()
		if idxErr != nil {
			return fmt.Errorf("reading enum type name index: %w", idxErr)
		}
		c.addReference(internalNameFromDescriptor(c.utf8(typeNameIndex)))
		err = c.r.skip(2) // const_name_index
	case 'c':
		nameIndex, idxErr := c.r.u2()
		if idxErr != nil {
			return fmt.Errorf("reading class info index: %w", idxErr)
		}
		c.addReference(internalNameFromDescriptor(c.utf8(nameIndex)))
	case '@':
		return c.readAnnotation()
	case '[':
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
