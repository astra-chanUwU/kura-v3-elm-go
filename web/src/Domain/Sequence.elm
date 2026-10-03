module Domain.Sequence exposing
    ( Sequence
    , empty
    , find
    , fromList
    , append
    , get
    , idAt
    , ids
    , inOrder
    , indexOf
    , isEmpty
    , length
    , member
    , range
    , slice
    , step
    , updateTags
    )

{-| The current search-result sequence shared by Grid, Loupe, Compare, Survey,
and the Filmstrip. Index lookups are constant time so 20,000 posts stay cheap.
-}

import Array exposing (Array)
import Dict exposing (Dict)
import Domain.Post exposing (PostSummary)
import Set exposing (Set)


type Sequence
    = Sequence
        { posts : Array PostSummary
        , indexById : Dict String Int
        }


empty : Sequence
empty =
    Sequence { posts = Array.empty, indexById = Dict.empty }


fromList : List PostSummary -> Sequence
fromList posts =
    Sequence
        { posts = Array.fromList posts
        , indexById =
            posts
                |> List.indexedMap (\index post -> ( post.id, index ))
                |> Dict.fromList
        }


append : List PostSummary -> Sequence -> Sequence
append posts sequence =
    let
        existing =
            List.filterMap (\index -> get index sequence) (List.range 0 (length sequence - 1))

        ( _, reversedAdditions ) =
            List.foldl
                (\post (seen, additions) ->
                    if Set.member post.id seen then
                        ( seen, additions )

                    else
                        ( Set.insert post.id seen, post :: additions )
                )
                ( Set.fromList (List.map .id existing), [] )
                posts
    in
    fromList (existing ++ List.reverse reversedAdditions)


length : Sequence -> Int
length (Sequence s) =
    Array.length s.posts


isEmpty : Sequence -> Bool
isEmpty sequence =
    length sequence == 0


get : Int -> Sequence -> Maybe PostSummary
get index (Sequence s) =
    Array.get index s.posts


indexOf : String -> Sequence -> Maybe Int
indexOf id (Sequence s) =
    Dict.get id s.indexById


member : String -> Sequence -> Bool
member id (Sequence s) =
    Dict.member id s.indexById


find : String -> Sequence -> Maybe PostSummary
find id sequence =
    indexOf id sequence |> Maybe.andThen (\index -> get index sequence)


idAt : Int -> Sequence -> Maybe String
idAt index sequence =
    get index sequence |> Maybe.map .id


{-| Move `delta` positions from `id`, clamped to the ends (no wrapping).
-}
step : Int -> String -> Sequence -> Maybe String
step delta id sequence =
    indexOf id sequence
        |> Maybe.andThen (\index -> idAt (clamp 0 (length sequence - 1) (index + delta)) sequence)


updateTags : String -> List String -> Sequence -> Sequence
updateTags postId tags sequence =
    case indexOf postId sequence of
        Just index ->
            case get index sequence of
                Just post ->
                    let
                        updated =
                            { post | tags = tags }

                    in
                    case sequence of
                        Sequence data ->
                            Sequence { data | posts = Array.set index updated data.posts }

                Nothing ->
                    sequence

        Nothing ->
            sequence


{-| Posts with their indexes from `from` (inclusive) to `to` (exclusive).
-}
slice : Int -> Int -> Sequence -> List ( Int, PostSummary )
slice from to (Sequence s) =
    let
        start =
            max 0 from
    in
    Array.slice start (min to (Array.length s.posts)) s.posts
        |> Array.toIndexedList
        |> List.map (\( offset, post ) -> ( start + offset, post ))


{-| Ids between two posts, inclusive, in sequence order.
-}
range : String -> String -> Sequence -> List String
range a b sequence =
    case ( indexOf a sequence, indexOf b sequence ) of
        ( Just i, Just j ) ->
            slice (min i j) (max i j + 1) sequence |> List.map (Tuple.second >> .id)

        _ ->
            []


ids : Sequence -> List String
ids (Sequence s) =
    Array.toList s.posts |> List.map .id


{-| The members of `set` that are in the sequence, in sequence order.
-}
inOrder : Set String -> Sequence -> List String
inOrder set sequence =
    set
        |> Set.toList
        |> List.filterMap (\id -> indexOf id sequence |> Maybe.map (\index -> ( index, id )))
        |> List.sortBy Tuple.first
        |> List.map Tuple.second
