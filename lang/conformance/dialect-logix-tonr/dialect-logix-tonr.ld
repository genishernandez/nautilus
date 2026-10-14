PROGRAM TonrLd
(* TONR in ladder: rung power drives IN; the pulse rung is the page's
   t:TONR(PT := T#1S, Reset := t.Q) on the rail. *)
VAR_EXTERNAL
    IN     : BOOL;
    PT     : TIME;
    Reset  : BOOL;
    ld_Q   : BOOL;
    ld_ET  : TIME;
    ld_P   : BOOL;
    ld_PET : TIME;
END_VAR
VAR
    t : TONR;
    p : TONR;
END_VAR
LD
  RUNG held
    IN t:TONR(PT := PT, Reset := Reset, ET => ld_ET) ( ld_Q )
  RUNG pulse
    p:TONR(PT := T#1S, Reset := p.Q, ET => ld_PET) ( ld_P )
END_LD
END_PROGRAM
