PROGRAM ScopeLd
(* No VAR_EXTERNAL: the rungs name manifest tags directly. *)
LD
  RUNG on
    Run ( ld_On )
  RUNG out
    { ld_Out := Gain * 2.0; ld_N := Cnt + 1 }
END_LD
END_PROGRAM
