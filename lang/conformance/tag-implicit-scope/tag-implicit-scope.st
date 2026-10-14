PROGRAM ScopeSt
(* No VAR_EXTERNAL: Run, Gain, Cnt and the st_ outputs are manifest tags. *)
st_On  := Run;
st_Out := Gain * 2.0;
st_N   := Cnt + 1;
END_PROGRAM
