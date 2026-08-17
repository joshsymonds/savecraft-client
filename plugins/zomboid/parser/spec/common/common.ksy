meta:
  id: common
types:
  # UTF-8 string with u2be length prefix
  # GameWindow.WriteString / ReadString / WriteStringUTF / ReadStringUTF
  string_utf:
    seq:
      - id: len
        type: u2be
      - id: value
        type: str
        size: len
        encoding: UTF-8

  # Line-ending (0x0A) terminated string
  # IsoLot.readString / writeString
  string_l:
    seq:
      - id: value
        terminator: 0x0A

  strz_utf:
    seq:
      - id: value
        type: strz
        encoding: UTF-8

  # lotheader, lotpack: little-endian
  # IsoLot.readInt / writeInt (s4le)
  # IsoLot.readShort / writeShort (s2le)

  # chunk, vehicle: big-endian
  # IsoChunk related types

  # KahluaTable (big-endian), assume world_version is >= 25
  # se.krka.kahlua.j2se.KahluaTableImpl.save / load
  ktable:
    seq:
      - id: num_entries
        type: s4be
      - id: entries
        type: kv_pair
        repeat: expr
        repeat-expr: num_entries

  kv_pair:
    seq:
      - id: key
        type: kobject
      - id: value
        type: kobject

  kobject:
    seq:
      - id: type
        type: u1
        valid:
          expr: _ >= 0 and _ <= 3
      - id: value
        type:
          switch-on: type
          cases:
            0: string_utf
            2: ktable
            1: f8be
            3: u1

  serialized_class_header:
    seq:
      - id: serialize
        type: u1
        valid:
          expr: _ == 0 or _ == 1
      - id: raw_class_id
        type: u1
        if: serialize == 1
    instances:
      class_id:
        value: '(serialize == 1) ? raw_class_id.as<s4> : -1'

  blob:
    seq:
      - id: data
        size-eos: true

  color_rgb:
    seq:
      - id: r
        type: u1
      - id: g
        type: u1
      - id: b
        type: u1

  color_rgba:
    seq:
      - id: r
        type: u1
      - id: g
        type: u1
      - id: b
        type: u1
      - id: a
        type: u1

  id_or_name_s4be:
    params:
      - id: is_id
        type: bool
    seq:
      - id: value
        type:
          switch-on: is_id
          cases:
            true: s4be
            false: string_utf

  id_or_name_u1:
    params:
      - id: is_id
        type: bool
    seq:
      - id: value
        type:
          switch-on: is_id
          cases:
            true: u1
            false: string_utf

  unknown:
    params:
      - id: type_id
        type: u4
    seq:
      # fast fail for unknown class IDs and raise the class id
      - id: explode
        size: type_id * 100000
        if: true

  bytes_eos:
    seq:
      - id: data
        size-eos: true
    instances:
      size:
        value: data.size

  bit_mask_be:
    params:
      - id: num_bytes
        type: u4
    seq:
      - id: data
        type: bit_mask_eos
        size: num_bytes * 1
    instances:
      bits:
        value: data.bits
      flags:
        io: data._io
        pos: 0
        type:
          switch-on: num_bytes
          cases:
            1: u1
            2: u2be
            4: u4be
            8: u8be
        if: num_bytes == 1 or num_bytes == 2 or num_bytes == 4 or num_bytes == 8

  bit_mask_eos:
    seq:
      - id: data
        type: mask_u1
        repeat: eos
    instances:
       sum_arr:
         type: 'reduce_sum(data[_index].bits, (_index == 0) ? 0 : sum_arr[_index - 1].result)'
         repeat: expr
         repeat-expr: data.size
       bits:
         value: '(data.size == 0) ? 0 : sum_arr.last.result'

  reduce_sum:
    params:
      - id: a
        type: u4
      - id: b
        type: u4
    instances:
      result:
        value: a + b

  mask_u1:
    seq:
      - id: raw
        type: u1
    instances:
      s2:
        value: (raw & 0x55) + ((raw >> 1) & 0x55)
      s4:
        value: (s2 & 0x33) + ((s2 >> 2) & 0x33)
      bits:
        value: (s4 & 0x0F) + ((s4 >> 4) & 0x0F)

  # zombie.network.id.ObjectID.load / save (base)
  # zombie.network.id.ObjectID.ObjectIDInteger.load / save
  # zombie.network.id.ObjectID.ObjectIDShort.load / save
  network_id:
    params:
      - id: size_bytes
        type: u1
    seq:
      - id: value
        if: size_bytes == 2 or size_bytes == 4
        type:
          switch-on: size_bytes
          cases:
            2: u2be
            4: u4be
      - id: base
        type: u1

  remaining_bytes:
    params:
      - id: expected_size
        type: s4
    seq:
      - id: data
        type: data_eos
      - id: size
        size: 0
        process: copy_data.u4le(data.data.size)
        type:
          switch-on: expected_size >= 0
          cases:
            true: remaining_bytes_size_check(expected_size)
            false: empty

  remaining_bytes_size_check:
    params:
      - id: expected
        type: u4
    seq:
      - id: value
        type: u4le
        valid:
          expr: _ == expected

  data_eos:
    seq:
      - id: data
        type: u1
        repeat: eos

  empty:
    seq: []
