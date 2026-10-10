module Domain.Query exposing (addTerm, excludeTerm, order, removeTerm, setOrder, terms)

{-| Query text helpers for the FilterBar and Inspector tag links.

Until the KuraQL parser lands, terms follow the PostgreSQL web search syntax the
API already accepts: whitespace-separated words, `"quoted phrases"`, and a
leading `-` to exclude.

-}


terms : String -> List String
terms query =
    let
        flush current acc =
            if List.isEmpty current then
                acc

            else
                String.fromList (List.reverse current) :: acc

        step char ( current, acc, quoted ) =
            if char == '"' then
                ( char :: current, acc, not quoted )

            else if not quoted && isSpace char then
                ( [], flush current acc, quoted )

            else
                ( char :: current, acc, quoted )

        ( last, collected, _ ) =
            String.foldl step ( [], [], False ) query
    in
    List.reverse (flush last collected)


addTerm : String -> String -> String
addTerm tag query =
    replaceTerm (quote tag) ("-" ++ quote tag) query


excludeTerm : String -> String -> String
excludeTerm tag query =
    replaceTerm ("-" ++ quote tag) (quote tag) query


removeTerm : Int -> String -> String
removeTerm index query =
    terms query
        |> List.indexedMap Tuple.pair
        |> List.filter (\( i, _ ) -> i /= index)
        |> List.map Tuple.second
        |> String.join " "


replaceTerm : String -> String -> String -> String
replaceTerm wanted opposite query =
    let
        kept =
            terms query |> List.filter (\term -> term /= wanted && term /= opposite)
    in
    String.join " " (kept ++ [ wanted ])


quote : String -> String
quote tag =
    if String.any isSpace tag then
        "\"" ++ tag ++ "\""

    else
        tag


isSpace : Char -> Bool
isSpace char =
    char == ' ' || char == '\t' || char == '\n' || char == '\u{000D}'


order : String -> String
order query =
    if List.member "order:score" (terms query) then
        "score"

    else
        "newest"


setOrder : String -> String -> String
setOrder requested query =
    let
        kept =
            terms query |> List.filter (\term -> not (String.startsWith "order:" term))
    in
    String.join " "
        (if requested == "score" then
            kept ++ [ "order:score" ]

         else
            kept
        )
