PROGRAM RTrigLd
(* R_TRIG in ladder: rung power drives CLK, Q continues to the coil. *)
VAR_EXTERNAL
    CLK  : BOOL;
    ld_Q : BOOL;
END_VAR
VAR
    t : R_TRIG;
END_VAR
LD
  RUNG rtrig
    CLK t:R_TRIG() ( ld_Q )
END_LD
END_PROGRAM
