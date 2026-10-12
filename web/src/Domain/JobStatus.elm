module Domain.JobStatus exposing (isTerminal)

{-| Status facts shared by job polling and the pure upload queue.
-}


isTerminal : String -> Bool
isTerminal status =
    status == "succeeded" || status == "failed" || status == "cancelled"
