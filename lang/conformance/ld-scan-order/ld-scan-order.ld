PROGRAM OrderLd
(* ld_X is a coil read as a contact on the NEXT rung; ld_W is a coil read
   as a contact on the rung ABOVE it. *)
VAR_EXTERNAL
    A    : BOOL;
    ld_X : BOOL;
    ld_Y : BOOL;
    ld_V : BOOL;
    ld_W : BOOL;
END_VAR
LD
  RUNG writeX
    A ( ld_X )
  RUNG readX
    ld_X ( ld_Y )
  RUNG readW
    ld_W ( ld_V )
  RUNG writeW
    A ( ld_W )
END_LD
END_PROGRAM
