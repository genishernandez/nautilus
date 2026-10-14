PROGRAM PartLd
VAR_EXTERNAL
    Alarms    : DINT;
    ld_Bit3   : BOOL;
    ld_Packed : WORD;
    ld_Bit3L  : BOOL;
    ld_PackedL : WORD;
    ld_HiByteGt : BOOL;
END_VAR
LD
  RUNG bit3
    Alarms.%X3 ( ld_Bit3 )
  RUNG pack0
    Alarms.%X3 ( ld_Packed.%X0 )
  RUNG pack15
    Alarms.%X31 ( ld_Packed.%X15 )
  RUNG bit3logix
    Alarms.3 ( ld_Bit3L )
  RUNG packlogix
    Alarms.31 ( ld_PackedL.15 )
  RUNG hibyte
    GT(Alarms.%B3, 100) ( ld_HiByteGt )
END_LD
END_PROGRAM
